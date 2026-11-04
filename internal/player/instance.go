package player

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is the connection state of one camera.
type State int32

const (
	Disconnected State = iota
	Connecting
	Playing
	Reconnecting
	Error
)

func (s State) String() string {
	switch s {
	case Disconnected:
		return "Disconnected"
	case Connecting:
		return "Connecting"
	case Playing:
		return "Playing"
	case Reconnecting:
		return "Reconnecting"
	case Error:
		return "Error"
	}
	return "?"
}

// Reconnect timing: 2 s, 4 s, 8 s, 16 s, 30 s, 30 s ...
const (
	initialBackoff = 2 * time.Second
	maxBackoff     = 30 * time.Second
)

// Callbacks are called from other threads. They must be thread safe
// (the window code only posts a message to the UI thread).
type Callbacks struct {
	StateChanged func(idx int)
	// Action is "toggle" (double click) or "back" (Esc key in the video).
	Action func(idx int, action string)
	Logf   func(format string, args ...any)
}

// Instance is one libmpv player for one camera.
type Instance struct {
	idx   int
	url   string
	scale float64 // DPI scale for the OSD text
	h     uintptr
	cb    Callbacks

	done chan struct{} // closed when the event loop has ended

	mu           sync.Mutex
	state        State
	detail       string
	closing      bool
	reconnecting bool // a stream was lost or failed since the last Playing
	backoff      time.Duration
	timer        *time.Timer // not nil while a reconnect is scheduled
	w, h2        int         // size of the video window in pixels
}

// New creates the player, embeds it in the window wid and starts the stream.
func New(idx int, url string, wid uintptr, scale float64, cb Callbacks) (*Instance, error) {
	h := mpvCreate()
	if h == 0 {
		return nil, fmt.Errorf("mpv_create failed")
	}
	i := &Instance{idx: idx, url: url, h: h, cb: cb, scale: scale, done: make(chan struct{}), backoff: initialBackoff}

	set := func(name, value string, required bool) error {
		if rc := mpvSetOption(h, name, value); rc < 0 {
			i.logf("option %s=%s: %s", name, value, errString(rc))
			if required {
				return fmt.Errorf("option %s: %s", name, errString(rc))
			}
		}
		return nil
	}
	if err := set("wid", strconv.FormatUint(uint64(wid), 10), true); err != nil {
		mpvDestroy(h)
		return nil, err
	}
	for _, o := range options {
		set(o[0], o[1], false)
	}
	if rc := mpvInitialize(h); rc < 0 {
		mpvDestroy(h)
		return nil, fmt.Errorf("mpv_initialize: %s", errString(rc))
	}
	mpvRequestLog(h, "error")
	// Mouse and keyboard events inside the video window come back to us as
	// client messages, because the video window is owned by libmpv.
	mpvCommand(h, "keybind", "MBTN_LEFT_DBL", "script-message tcv-toggle")
	mpvCommand(h, "keybind", "ESC", "script-message tcv-back")

	go i.loop()
	i.connect()
	return i, nil
}

// options are set before mpv_initialize. Unknown options are only logged.
var options = [][2]string{
	{"config", "no"}, // do not read the user's mpv.conf
	{"load-scripts", "no"},
	{"ytdl", "no"},
	{"terminal", "no"},
	{"idle", "yes"}, // keep the player alive between connection attempts
	{"force-window", "yes"},
	{"title", "Tiny Cam Viewer"},
	{"profile", "fast,low-latency"},
	{"vo", "gpu"},
	{"gpu-api", "d3d11"},
	{"hwdec", "auto"},
	{"rtsp-transport", "tcp"},
	{"aid", "no"},
	{"ao", "null"},
	{"osc", "no"},
	{"osd-level", "0"},
	{"keep-open", "no"},
	{"input-default-bindings", "no"},
	{"input-media-keys", "no"},
	{"cache", "no"},
	{"demuxer-max-bytes", "2MiB"},
	{"demuxer-max-back-bytes", "0"},
	{"network-timeout", "10"},
	{"demuxer-lavf-o", "timeout=8000000"}, // RTSP socket timeout (microseconds): connect and stalled streams
	{"background-color", "#000000"},
}

func (i *Instance) logf(format string, args ...any) {
	if i.cb.Logf != nil {
		i.cb.Logf("cam %d: "+format, append([]any{i.idx + 1}, args...)...)
	}
}

// Snapshot returns the state and a short detail text for the status badge.
func (i *Instance) Snapshot() (State, string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.state, i.detail
}

func (i *Instance) setState(s State, detail string) {
	i.mu.Lock()
	changed := i.state != s || i.detail != detail
	i.state, i.detail = s, detail
	closing := i.closing
	i.mu.Unlock()
	if changed && !closing {
		i.updateOSD(s, detail)
		if i.cb.StateChanged != nil {
			i.cb.StateChanged(i.idx)
		}
	}
}

// connect starts (or restarts) the stream.
func (i *Instance) connect() {
	i.mu.Lock()
	if i.closing {
		i.mu.Unlock()
		return
	}
	i.timer = nil
	st := Connecting
	if i.reconnecting {
		st = Reconnecting
	}
	i.mu.Unlock()
	i.setState(st, "")
	if rc := mpvCommand(i.h, "loadfile", i.url, "replace"); rc < 0 {
		i.fail("loadfile: " + errString(rc))
	}
}

// fail is called when a connection attempt failed or the stream was lost.
// It schedules exactly one reconnect (exponential backoff).
func (i *Instance) fail(msg string) {
	i.mu.Lock()
	if i.closing || i.timer != nil {
		i.mu.Unlock()
		return
	}
	wasPlaying := i.state == Playing
	delay := i.backoff
	i.backoff = min(i.backoff*2, maxBackoff)
	i.reconnecting = true
	i.timer = time.AfterFunc(delay, i.connect)
	i.mu.Unlock()

	i.logf("%s; retry in %s", msg, delay)
	retry := fmt.Sprintf("retry in %ds", int(delay.Seconds()))
	if wasPlaying {
		i.setState(Reconnecting, "lost, "+retry)
	} else {
		i.setState(Error, shorten(msg, 60)+", "+retry)
	}
}

func shorten(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// loop reads libmpv events. It sleeps inside libmpv until an event arrives.
func (i *Instance) loop() {
	defer close(i.done)
	for {
		ev := waitEvent(i.h, 3600)
		switch ev.id {
		case evShutdown:
			return
		case evLogMessage:
			// struct: prefix, level, text (char*), log_level
			text := strings.TrimSpace(goString(readPtr(ev.data, 16)))
			prefix := goString(readPtr(ev.data, 0))
			i.logf("[%s] %s", prefix, text)
		case evPlaybackRestart:
			i.onPlaying()
		case evEndFile:
			reason := readInt32(ev.data, 0)
			code := readInt32(ev.data, 4)
			if reason != endEOF && reason != endError {
				continue // stop/quit/redirect: caused by us
			}
			msg := "stream ended"
			if reason == endError {
				msg = errString(code)
				if code == errLoadingFailed || code == errUnknownFormat {
					msg = "cannot connect"
				}
			}
			i.fail(msg)
		case evClientMessage:
			n := readInt32(ev.data, 0)
			if n < 1 || i.cb.Action == nil {
				continue
			}
			switch goString(readPtr(readPtr(ev.data, 8), 0)) {
			case "tcv-toggle":
				i.cb.Action(i.idx, "toggle")
			case "tcv-back":
				i.cb.Action(i.idx, "back")
			}
		}
	}
}

// onPlaying runs when the first video frame is shown.
func (i *Instance) onPlaying() {
	i.mu.Lock()
	if i.closing {
		i.mu.Unlock()
		return
	}
	i.backoff = initialBackoff
	i.reconnecting = false
	i.mu.Unlock()

	// Check that hardware decoding is really active.
	hw := mpvGetProperty(i.h, "hwdec-current")
	mode := "SW"
	if hw != "" && hw != "no" {
		mode = "HW"
	} else {
		hw = "no"
	}
	i.logf("playing, hwdec=%s", hw)
	i.setState(Playing, mode)
}

// Stop asks the player to quit. Call Destroy afterwards.
func (i *Instance) Stop() {
	i.mu.Lock()
	i.closing = true
	if i.timer != nil {
		i.timer.Stop()
		i.timer = nil
	}
	i.mu.Unlock()
	mpvCommand(i.h, "quit")
	mpvWakeup(i.h)
}

// Destroy frees all libmpv resources. Call it after Stop.
func (i *Instance) Destroy() {
	select {
	case <-i.done:
		mpvDestroy(i.h)
	case <-time.After(3 * time.Second):
		i.logf("player did not stop in time")
	}
}

// CloseAll stops all players in parallel and frees them.
func CloseAll(list []*Instance) {
	var wg sync.WaitGroup
	for _, p := range list {
		p.Stop()
	}
	for _, p := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Destroy()
		}()
	}
	wg.Wait()
}
