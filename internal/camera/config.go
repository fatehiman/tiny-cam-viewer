// Package camera reads and validates the cameras.conf file.
package camera

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// MaxCameras is the number of cells in the 4x2 grid.
const MaxCameras = 8

// ExampleConf is written when cameras.conf does not exist.
const ExampleConf = `# Tiny Cam Viewer - camera list
#
# One RTSP URL per line. Up to 8 cameras. Camera numbers follow the line order.
# Empty lines and lines that start with # are ignored.
# After you edit this file, use "Reload cameras" in the tray menu.
#
# Examples (remove the # and use your own camera address and login):
#
# rtsp://192.168.1.18:554/user=admin&password=YOUR_PASSWORD&channel=1&stream=1.sdp?real_stream
# rtsp://admin:YOUR_PASSWORD@192.168.1.20:554/Streaming/Channels/101
#
# Two streams per camera: {normal:X} is used in the grid, {full:Y} is used when
# the camera is shown fullscreen. They can be anywhere in the line.
#
# rtsp://192.168.1.18:554/user=admin&password=YOUR_PASSWORD&channel=1&stream={normal:1}{full:2}.sdp?real_stream
`

// Entry is one camera line from the config file.
type Entry struct {
	Number  int    // 1-based camera number (order of the non-empty lines)
	URL     string // the RTSP URL for the grid (thumbnail) view
	FullURL string // the RTSP URL for the fullscreen view; empty = same stream as URL
	Err     string // not empty when a URL is not valid
}

// modeRe finds {normal:X} and {full:X} anywhere in a line.
var modeRe = regexp.MustCompile(`(?i)\{(normal|full):([^{}]*)\}`)

// Expand does the simple text processing for {normal:X} and {full:X}.
// In the grid URL, {normal:X} becomes X and {full:X} becomes nothing.
// In the fullscreen URL, it is the other way round.
// If the line has no {full:...}, or both URLs are the same, full is "" (one stream for both views).
func Expand(line string) (normal, full string) {
	pick := func(mode string) func(string) string {
		return func(m string) string {
			sm := modeRe.FindStringSubmatch(m)
			if strings.EqualFold(sm[1], mode) {
				return sm[2]
			}
			return ""
		}
	}
	normal = modeRe.ReplaceAllStringFunc(line, pick("normal"))
	hasFull := false
	for _, sm := range modeRe.FindAllStringSubmatch(line, -1) {
		if strings.EqualFold(sm[1], "full") {
			hasFull = true
		}
	}
	if !hasFull {
		return normal, ""
	}
	full = modeRe.ReplaceAllStringFunc(line, pick("full"))
	if full == normal {
		full = ""
	}
	return normal, full
}

// Load reads the config file. If the file does not exist, it creates an
// example file and returns created=true.
func Load(path string) (entries []Entry, created bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if werr := os.WriteFile(path, []byte(ExampleConf), 0o644); werr != nil {
			return nil, false, werr
		}
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "ï»¿") // UTF-8 BOM
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(entries) >= MaxCameras {
			break // extra lines are ignored
		}
		e := Entry{Number: len(entries) + 1}
		e.URL, e.FullURL = Expand(line)
		if verr := Validate(e.URL); verr != nil {
			e.Err = verr.Error()
		} else if e.FullURL != "" {
			if verr := Validate(e.FullURL); verr != nil {
				e.Err = "fullscreen URL: " + verr.Error()
			}
		}
		entries = append(entries, e)
	}
	return entries, false, sc.Err()
}

// Validate checks that s looks like an RTSP URL.
func Validate(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return errors.New("invalid URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "rtsp", "rtsps":
	default:
		return fmt.Errorf("URL must start with rtsp://")
	}
	if u.Hostname() == "" {
		return errors.New("URL has no host")
	}
	return nil
}

var secretRe = regexp.MustCompile(`(?i)((?:password|passwd|pwd|pass)=)[^&?#\s]*`)
var userinfoRe = regexp.MustCompile(`(://[^/:@\s]*:)[^@/\s]*@`)

// Redact hides passwords in a URL so it is safe to write to a log file.
func Redact(s string) string {
	s = secretRe.ReplaceAllString(s, "${1}***")
	return userinfoRe.ReplaceAllString(s, "${1}***@")
}
