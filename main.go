// Tiny Cam Viewer: a lightweight 8-channel RTSP viewer for Windows.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows"

	"github.com/fatehiman/tiny-cam-viewer/internal/camera"
	"github.com/fatehiman/tiny-cam-viewer/internal/player"
	"github.com/fatehiman/tiny-cam-viewer/internal/window"
)

const appName = "Tiny Cam Viewer"

func init() {
	runtime.LockOSThread() // the Win32 message loop must stay on the main thread
}

func main() { os.Exit(run()) }

func run() int {
	// Only one instance. MessageBox works before any UI exists.
	name, _ := windows.UTF16PtrFromString(`Local\TinyCamViewerSingleInstance`)
	if _, err := windows.CreateMutex(nil, false, name); err == windows.ERROR_ALREADY_EXISTS {
		window.MessageBox(appName+" is already running.", appName, true)
		return 1
	}

	exe, err := os.Executable()
	if err != nil {
		window.MessageBox("Cannot find the program folder: "+err.Error(), appName, true)
		return 1
	}
	dir := filepath.Dir(exe)

	logf := openLog(filepath.Join(dir, "tiny-cam-viewer.log"))
	logf("start, exe=%s", exe)

	dll := filepath.Join(dir, "libmpv-2.dll")
	if err := player.Load(dll); err != nil {
		logf("cannot load %s: %v", dll, err)
		window.MessageBox(fmt.Sprintf("Cannot load libmpv-2.dll from:\n%s\n\n%v\n\nRun scripts\fetch-libmpv.ps1 or see README.md.", dir, err), appName, true)
		return 1
	}
	return window.Run(dir, logf)
}

// openLog opens a small log file next to the exe. Passwords are hidden.
// If the file cannot be opened, logging is switched off.
func openLog(path string) func(string, ...any) {
	if st, err := os.Stat(path); err == nil && st.Size() > 512*1024 {
		os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return func(string, ...any) {}
	}
	l := log.New(f, "", log.Ldate|log.Ltime)
	return func(format string, args ...any) {
		l.Print(camera.Redact(fmt.Sprintf(format, args...)))
	}
}
