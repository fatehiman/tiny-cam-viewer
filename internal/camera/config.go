// Package camera reads and validates the cameras.conf file.
package camera

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
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
#
# Optional aspect ratio per stream (after a comma):
#   {normal:4:3}{full:16:9}  - different ratios for grid vs fullscreen
#   16:9                     - same ratio for both views
#   {normal:640x480}{full:1920x1080}  - explicit dimensions also work
#
# rtsp://192.168.1.18:554/user=admin&password=YOUR_PASSWORD&channel=1&stream={normal:1}{full:2}.sdp?real_stream,{normal:4:3}{full:16:9}
`

// Entry is one camera line from the config file.
type Entry struct {
	Number      int    // 1-based camera number (order of the non-empty lines)
	URL         string // the RTSP URL for the grid (thumbnail) view
	FullURL     string // the RTSP URL for the fullscreen view; empty = same stream as URL
	NormalAR    string // aspect ratio for normal view (e.g., "4:3", "16:9", "640x480")
	FullAR      string // aspect ratio for fullscreen view; empty = same as NormalAR
	Err         string // not empty when a URL is not valid
}

// WindowState stores the main window position, size, and state.
type WindowState struct {
	X          int  // window left position (normal state)
	Y          int  // window top position (normal state)
	Width      int  // window width (normal state)
	Height     int  // window height (normal state)
	Maximized  bool // whether window was maximized
	Monitor    int  // monitor index (0 = primary)
	HasState   bool // whether valid state was saved
}

// modeRe finds {normal:X} and {full:X} anywhere in a line.
var modeRe = regexp.MustCompile(`(?i)\{(normal|full):([^{}]*)\}`)

// arRe finds {normal:X} and {full:X} in the aspect ratio spec.
var arRe = regexp.MustCompile(`(?i)\{(normal|full):([^{}]*)\}`)

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

// parseAspectRatio parses the aspect ratio specification (the part after the comma).
// Formats supported:
//   - "4x3" or "16:9" - single aspect ratio for both views
//   - "{normal:4x3}{full:16x9}" - different aspect ratios for normal and full
//   - "{normal:640x480}{full:1024x768}" - explicit dimensions
// Returns (normalAR, fullAR). Empty string means no override.
func parseAspectRatio(spec string) (normalAR, fullAR string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", ""
	}

	// Check if it contains {normal:...} or {full:...} markers
	hasMarkers := arRe.MatchString(spec)

	if !hasMarkers {
		// Single aspect ratio for both views
		return spec, spec
	}

	// Parse markers
	pick := func(mode string) func(string) string {
		return func(m string) string {
			sm := arRe.FindStringSubmatch(m)
			if strings.EqualFold(sm[1], mode) {
				return sm[2]
			}
			return ""
		}
	}
	normalAR = arRe.ReplaceAllStringFunc(spec, pick("normal"))
	hasFull := false
	for _, sm := range arRe.FindAllStringSubmatch(spec, -1) {
		if strings.EqualFold(sm[1], "full") {
			hasFull = true
		}
	}
	if !hasFull {
		fullAR = normalAR
	} else {
		fullAR = arRe.ReplaceAllStringFunc(spec, pick("full"))
		if fullAR == normalAR {
			fullAR = ""
		}
	}
	return normalAR, fullAR
}

// normalizeAR converts various aspect ratio formats to "W:H" format for mpv.
// Accepts: "4:3", "16:9", "4x3", "640x480", "1920x1080", "1.333", "1.777778"
// Returns empty string if input is empty or invalid.
func normalizeAR(ar string) string {
	ar = strings.TrimSpace(ar)
	if ar == "" {
		return ""
	}
	// Replace 'x' with ':' for consistency
	ar = strings.ReplaceAll(ar, "x", ":")
	// If it contains ':', it's already in W:H format
	if strings.Contains(ar, ":") {
		parts := strings.Split(ar, ":")
		if len(parts) == 2 {
			w, err1 := strconv.ParseFloat(parts[0], 64)
			h, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil && h != 0 {
				return fmt.Sprintf("%.6f", w/h)
			}
		}
		return ""
	}
	// Try parsing as a float (e.g., "1.333", "1.777778")
	if f, err := strconv.ParseFloat(ar, 64); err == nil && f > 0 {
		return fmt.Sprintf("%.6f", f)
	}
	return ""
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

			// Split line into URL part and optional aspect ratio part (after comma)
			urlPart := line
			arPart := ""
			if idx := strings.LastIndex(line, ","); idx != -1 {
				// Check if the comma is inside {normal:...} or {full:...} - if so, don't split
				beforeComma := line[:idx]
				openBraces := strings.Count(beforeComma, "{")
				closeBraces := strings.Count(beforeComma, "}")
				if openBraces == closeBraces {
					urlPart = strings.TrimSpace(line[:idx])
					arPart = strings.TrimSpace(line[idx+1:])
				}
			}

			e := Entry{Number: len(entries) + 1}
			e.URL, e.FullURL = Expand(urlPart)
			e.NormalAR, e.FullAR = parseAspectRatio(arPart)
			// Normalize aspect ratios for mpv (convert to float ratio string)
			e.NormalAR = normalizeAR(e.NormalAR)
			e.FullAR = normalizeAR(e.FullAR)

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

// LoadWindowState reads the window state from cameras.conf (stored as special comment lines).
func LoadWindowState(path string) (WindowState, error) {
	var ws WindowState
	f, err := os.Open(path)
	if err != nil {
		return ws, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "# window-state:") {
			parseWindowState(line, &ws)
			ws.HasState = true
		}
	}
	return ws, sc.Err()
}

func parseWindowState(line string, ws *WindowState) {
	// Format: # window-state: x=100,y=100,w=800,h=600,maximized=true,monitor=0
	parts := strings.Split(strings.TrimPrefix(line, "# window-state:"), ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "x=") {
			ws.X, _ = strconv.Atoi(strings.TrimPrefix(p, "x="))
		} else if strings.HasPrefix(p, "y=") {
			ws.Y, _ = strconv.Atoi(strings.TrimPrefix(p, "y="))
		} else if strings.HasPrefix(p, "w=") {
			ws.Width, _ = strconv.Atoi(strings.TrimPrefix(p, "w="))
		} else if strings.HasPrefix(p, "h=") {
			ws.Height, _ = strconv.Atoi(strings.TrimPrefix(p, "h="))
		} else if strings.HasPrefix(p, "maximized=") {
			ws.Maximized, _ = strconv.ParseBool(strings.TrimPrefix(p, "maximized="))
		} else if strings.HasPrefix(p, "monitor=") {
			ws.Monitor, _ = strconv.Atoi(strings.TrimPrefix(p, "monitor="))
		}
	}
}

// SaveWindowState writes the window state to cameras.conf as a special comment line.
// It preserves all other content in the file.
func SaveWindowState(path string, ws WindowState) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	
	// Remove existing window-state lines
	var newLines []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "# window-state:") {
			newLines = append(newLines, line)
		}
	}
	
	// Add new window-state line if we have valid state
	if ws.HasState && ws.Width > 0 && ws.Height > 0 {
		stateLine := fmt.Sprintf("# window-state: x=%d,y=%d,w=%d,h=%d,maximized=%v,monitor=%d",
			ws.X, ws.Y, ws.Width, ws.Height, ws.Maximized, ws.Monitor)
		newLines = append(newLines, stateLine)
	}
	
	return os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0o644)
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
