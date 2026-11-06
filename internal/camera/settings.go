package camera

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SettingsFile is the name of the file where the app saves its own settings.
// It is separate from cameras.conf, so the app never rewrites the file that
// the user edits (and that holds the passwords).
const SettingsFile = "settings.conf"

// Picture is the brightness and contrast of one camera (-100 .. 100, 0 = normal).
type Picture struct {
	Brightness int
	Contrast   int
}

// WindowState is the saved main window: the normal (not maximized) rectangle,
// the maximized flag and the monitor number.
type WindowState struct {
	X, Y          int
	Width, Height int
	Maximized     bool
	Monitor       int  // monitor index, 0 = first
	Valid         bool // a usable state was saved
}

// Settings is everything the app remembers between runs.
type Settings struct {
	Window WindowState
	Cams   [MaxCameras]Picture // index = camera number - 1
}

func clampPic(v int) int { return max(-100, min(100, v)) }

// LoadSettings reads settings.conf from dir. A missing or broken file gives
// empty settings (no error): the app then uses its defaults.
func LoadSettings(dir string) Settings {
	var s Settings
	f, err := os.Open(filepath.Join(dir, SettingsFile))
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		n, err := strconv.Atoi(val)
		if err != nil {
			continue
		}
		switch key {
		case "window.x":
			s.Window.X = n
		case "window.y":
			s.Window.Y = n
		case "window.width":
			s.Window.Width = n
		case "window.height":
			s.Window.Height = n
		case "window.maximized":
			s.Window.Maximized = n != 0
		case "window.monitor":
			s.Window.Monitor = n
		default:
			var cam int
			var prop string
			if _, err := fmt.Sscanf(key, "cam.%d.%s", &cam, &prop); err == nil && cam >= 1 && cam <= MaxCameras {
				switch prop {
				case "brightness":
					s.Cams[cam-1].Brightness = clampPic(n)
				case "contrast":
					s.Cams[cam-1].Contrast = clampPic(n)
				}
			}
		}
	}
	s.Window.Valid = s.Window.Width >= 200 && s.Window.Height >= 120
	return s
}

// Save writes settings.conf in dir (through a temporary file).
func (s Settings) Save(dir string) error {
	var b strings.Builder
	b.WriteString("# Tiny Cam Viewer - saved settings. Written by the app: do not edit while it runs.\n")
	w := s.Window
	if w.Valid {
		fmt.Fprintf(&b, "window.x=%d\nwindow.y=%d\nwindow.width=%d\nwindow.height=%d\nwindow.maximized=%d\nwindow.monitor=%d\n",
			w.X, w.Y, w.Width, w.Height, map[bool]int{true: 1, false: 0}[w.Maximized], w.Monitor)
	}
	for i, p := range s.Cams {
		if p.Brightness != 0 {
			fmt.Fprintf(&b, "cam.%d.brightness=%d\n", i+1, p.Brightness)
		}
		if p.Contrast != 0 {
			fmt.Fprintf(&b, "cam.%d.contrast=%d\n", i+1, p.Contrast)
		}
	}
	path := filepath.Join(dir, SettingsFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
