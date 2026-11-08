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
	// Action is "toggle" (double click), "back" (Esc key) or "menu" (right click) in the video.
	Action func(idx int, action string)
	Logf   func(format string, args ...any)
}

// Instance is one libmpv player for one camera.
type Instance struct {
	idx      int
	normal   string  // stream for the grid
	fullURL  string  // stream for fullscreen; "" = use the same stream
	normalAR string  // aspect ratio for normal view (float ratio string for mpv)
	fullAR   string  // aspect ratio for fullscreen view; "" = use normalAR
	scale    float64 // DPI scale for the OSD text
	h        uintptr
	cb       Callbacks

	done chan struct{} // closed when the event loop has ended

	mu           sync.Mutex
	full         bool // fullscreen view is active
	state        State
	detail       string
	closing      bool
	paused       bool // stream closed on purpose (another camera is fullscreen)
	reconnecting bool // a stream was lost or failed since the last Playing
	backoff      time.Duration
	timer        *time.Timer // not nil while a reconnect is scheduled
	w, h2        int         // size of the video window in pixels

	cmdMu sync.Mutex // orders loadfile/stop from different goroutines
}

// current returns the URL and aspect ratio for the active view. Call with i.mu held.
func (i *Instance) current() (string, string) {
	if i.full && i.fullURL != "" {
		return i.fullURL, i.fullAR
	}
	return i.normal, i.normalAR
}

// New creates the player, embeds it in the window wid and starts the stream.
// fullURL may be empty (the same stream is used in fullscreen).
// normalAR and fullAR are aspect ratio overrides for mpv (float ratio strings like "1.777778").
// brightness and contrast are -100..100 (0 = unchanged).
func New(idx int, url, fullURL, normalAR, fullAR string, wid uintptr, scale float64, brightness, contrast int, cb Callbacks) (*Instance, error) {
	h := mpvCreate()
	if h == 0 {
		return nil, fmt.Errorf("mpv_create failed")
	}
	i := &Instance{idx: idx, normal: url, fullURL: fullURL, normalAR: normalAR, fullAR: fullAR, h: h, cb: cb, scale: scale, done: make(chan struct{}), backoff: initialBackoff}

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
	set("brightness", strconv.Itoa(brightness), false)
	set("contrast", strconv.Itoa(contrast), false)
	if rc := mpvInitialize(h); rc < 0 {
		mpvDestroy(h)
		return nil, fmt.Errorf("mpv_initialize: %s", errString(rc))
	}
	mpvRequestLog(h, "error")
	// Mouse and keyboard events inside the video window come back to us as
	// client messages, because the video window is owned by libmpv.
	mpvCommand(h, "keybind", "MBTN_LEFT_DBL", "script-message tcv-toggle")
	mpvCommand(h, "keybind", "ESC", "script-message tcv-back")
	mpvCommand(h, "keybind", "MBTN_RIGHT", "script-message tcv-menu")
	mpvCommand(h, "keybind", "MBTN_LEFT", "script-message tcv-click") // the window checks if the contrast button was hit

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
	{"keepaspect", "yes"}, // preserve video aspect ratio with letterboxing
	{"rtsp-transport", "tcp"},
	{"aid", "no"},
	{"ao", "null"},
	{"osc", "no"},
	{"osd-level", "0"},
	{"keep-open", "no"},
	{"input-default-bindings", "no"},
	{"input-media-keys", "no"},
	// Live view: show each frame as soon as it is decoded, do not wait for its
	// timestamp. With timed playback every network hiccup added delay that never
	// went away, the input buffer filled up, mpv stopped reading the socket and
	// the camera dropped parts of frames (broken pictures).
	{"untimed", "yes"},
	{"cache", "no"},
	{"demuxer-max-bytes", "2MiB"},
	{"demuxer-max-back-bytes", "0"},
	{"network-timeout", "10"},
	// nobuffer: from profile low-latency (this line replaces the whole list, so
	// it must be repeated here). timeout: RTSP socket timeout in microseconds.
	{"demuxer-lavf-o", "fflags=+nobuffer,timeout=8000000"},
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
	i.cmdMu.Lock()
	defer i.cmdMu.Unlock()
	i.mu.Lock()
	if i.closing || i.paused {
		i.mu.Unlock()
		return
	}
	i.timer = nil
	st := Connecting
	if i.reconnecting {
		st = Reconnecting
	}
	url, ar := i.current()
	i.mu.Unlock()
	i.setState(st, "")
	if rc := mpvCommand(i.h, "loadfile", url, "replace"); rc < 0 {
		i.fail("loadfile: " + errString(rc))
	}
	// Set aspect ratio override if specified
	if ar != "" {
		mpvSetOption(i.h, "video-aspect-override", ar)
	}
}

// SetPicture changes brightness and contrast (-100..100) at once.
// libmpv keeps these values when the stream is restarted.
func (i *Instance) SetPicture(brightness, contrast int) {
	i.mu.Lock()
	closing := i.closing
	i.mu.Unlock()
	if closing {
		return
	}
	mpvCommand(i.h, "set", "brightness", strconv.Itoa(brightness))
	mpvCommand(i.h, "set", "contrast", strconv.Itoa(contrast))
}

// SetFull switches between the grid stream and the fullscreen stream.
// It does nothing if the camera has no separate fullscreen stream.
// The switch restarts the connection, so it runs in a goroutine.
func (i *Instance) SetFull(full bool) {
	i.mu.Lock()
	if i.closing || i.full == full || i.fullURL == "" {
		i.mu.Unlock()
		return
	}
	i.full = full
	if i.timer != nil { // a retry of the old stream is pending: replace it
		i.timer.Stop()
		i.timer = nil
	}
	i.backoff = initialBackoff
	i.mu.Unlock()
	i.logf("switch to %s stream", map[bool]string{true: "fullscreen", false: "grid"}[full])
	go i.connect()
}

// SetActive(false) closes the stream to save network bandwidth (used for the
// hidden cameras while another camera is fullscreen). SetActive(true) opens it
// again. Both run in a goroutine.
func (i *Instance) SetActive(on bool) {
	i.mu.Lock()
	if i.closing || i.paused == !on {
		i.mu.Unlock()
		return
	}
	i.paused = !on
	if i.timer != nil {
		i.timer.Stop()
		i.timer = nil
	}
	i.backoff = initialBackoff
	i.reconnecting = false
	i.mu.Unlock()
	if on {
		go i.connect()
		return
	}
	i.setState(Disconnected, "paused")
	go i.idle()
}

// idle closes the stream if the camera is still paused.
func (i *Instance) idle() {
	i.cmdMu.Lock()
	defer i.cmdMu.Unlock()
	i.mu.Lock()
	stop := i.paused && !i.closing
	i.mu.Unlock()
	if stop {
		i.logf("paused (other camera is fullscreen)")
		mpvCommand(i.h, "stop")
	}
}

// fail is called when a connection attempt failed or the stream was lost.
// It schedules exactly one reconnect (exponential backoff).
func (i *Instance) fail(msg string) {
	i.mu.Lock()
	if i.closing || i.paused || i.timer != nil {
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
			case "tcv-menu":
				i.cb.Action(i.idx, "menu")
			case "tcv-click":
				i.cb.Action(i.idx, "click")
			}
		}
	}
}

// onPlaying runs when the first video frame is shown.
func (i *Instance) onPlaying() {
	i.mu.Lock()
	if i.closing || i.paused {
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
