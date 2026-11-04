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
`

// Entry is one camera line from the config file.
type Entry struct {
	Number int    // 1-based camera number (order of the non-empty lines)
	URL    string // the RTSP URL as written in the file
	Err    string // not empty when the URL is not valid
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
		e := Entry{Number: len(entries) + 1, URL: line}
		if verr := Validate(line); verr != nil {
			e.Err = verr.Error()
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
