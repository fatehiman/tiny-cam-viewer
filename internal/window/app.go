// Package window is the native Win32 user interface: the main window, the
// 4x2 grid of camera cells, the status badges and the tray icon.
package window

import (
	"embed"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/fatehiman/tiny-cam-viewer/internal/camera"
	"github.com/fatehiman/tiny-cam-viewer/internal/player"
)

//go:embed assets/icon16.png assets/icon24.png assets/icon32.png assets/icon48.png
var iconFS embed.FS

const (
	appTitle = "Tiny Cam Viewer"
	cols     = 4
	rows     = 2

	wmAppQuit = wmApp + 4 // players are stopped: destroy the window
	wmAppDone = wmApp + 5 // old players are stopped: read the config again

	cmdToggleWindow = 1
	cmdReload       = 2
	cmdEditConf     = 3
	cmdExit         = 9

	actToggle = 1
	actBack   = 2
	actMenu   = 3
	actClick  = 4
)

// userData is GWLP_USERDATA (-21) as an index for Get/SetWindowLongPtr.
var userData = ^uintptr(20)

// slot is one cell of the grid.
type slot struct {
	cell   uintptr
	active bool // a camera line exists for this slot
	entry  camera.Entry
	inst   *player.Instance // nil if there is no player (yet)
	state  player.State     // used when inst is nil
	detail string
}

type app struct {
	hwnd      uintptr
	hInst     uintptr
	confPath  string
	logf      func(string, ...any)
	full      int // slot shown fullscreen, -1 = grid
	closing   bool
	reloading bool

	mu       sync.Mutex // protects slots[].inst, .state, .detail
	slots    [cols * rows]slot
	starting sync.WaitGroup

	scale    float64
	font     uintptr
	fontBold uintptr
	bgBrush  uintptr
	blackBr  uintptr

	nid            notifyIconData
	smallIcon      uintptr
	bigIcon        uintptr
	taskbarCreated uint32

	// Saved settings (window, brightness, contrast) and the picture panel
	set        camera.Settings
	dir        string // folder of the exe and the config files
	dragging   bool   // the user is moving or resizing the window
	pendingMax bool   // the window must be maximized when it is shown the first time
	panel      panel
}

var a *app

func scaled(v int) int {
	n := int(float64(v)*a.scale + .5)
	if n < 1 {
		n = 1
	}
	return n
}

// Run creates the UI and runs the message loop. It returns the exit code.
func Run(dir string, logf func(string, ...any)) int {
	runtime.LockOSThread()
	a = &app{confPath: filepath.Join(dir, "cameras.conf"), dir: dir, logf: logf, full: -1, scale: 1}
	a.hInst, _, _ = pGetModuleHandle.Call(0)

	a.set = camera.LoadSettings(dir)
	initControls()

	cursor, _, _ := pLoadCursor.Call(0, idcArrow)
	a.blackBr, _, _ = pGetStockObject.Call(4) // BLACK_BRUSH
	a.bgBrush, _, _ = pCreateSolidBrush.Call(rgb(28, 28, 28))
	tb, _, _ := pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	a.taskbarCreated = uint32(tb)

	register := func(name string, proc uintptr, style uint32, bg uintptr) bool {
		wc := wndClassEx{Style: style, WndProc: proc, Instance: a.hInst, Cursor: cursor,
			Background: bg, ClassName: utf16(name)}
		wc.Size = uint32(unsafe.Sizeof(wc))
		r, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
		return r != 0
	}
	if !register("TCVMain", syscall.NewCallback(mainProc), 0, 0) ||
		!register("TCVCell", syscall.NewCallback(cellProc), csDblClks, a.blackBr) ||
		!register("TCVOwner", syscall.NewCallback(ownerProc), 0, 0) ||
		!register("TCVPanel", syscall.NewCallback(panelProc), 0, 16) { // 16 = COLOR_BTNFACE + 1
		MessageBox("Cannot register the window classes.", appTitle, true)
		return 1
	}

	// A top-level window that has an owner never gets a taskbar button. The
	// owner is a hidden window, so the app lives only in the system tray.
	owner, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(utf16("TCVOwner"))), 0, wsPopup,
		0, 0, 0, 0, 0, 0, a.hInst, 0)

	// WS_OVERLAPPEDWINDOW without the minimize button: the close button (X)
	// hides the window to the tray. wsClipChildren is needed for the child cells.
	wsStyle := uintptr(wsOverlappedWindow&^wsMinimizeBox | wsClipChildren)

	hwnd, _, _ := pCreateWindowEx.Call(wsExNoParentNotify, uintptr(unsafe.Pointer(utf16("TCVMain"))),
		uintptr(unsafe.Pointer(utf16(appTitle))), wsStyle,
		cwUseDefault, cwUseDefault, 640, 360, owner, 0, a.hInst, 0)
	if hwnd == 0 {
		MessageBox("Cannot create the main window.", appTitle, true)
		return 1
	}
	a.hwnd = hwnd
	// Windows replaces the argument of the FIRST ShowWindow call of a process
	// with the "show" value from the start info (for example "minimized" when
	// DelayedTaskRunner starts us minimized). Use that first call up now, so
	// the later calls work as written.
	pShowWindow.Call(hwnd, swHide)

	// Put the window at its saved place and size (not shown yet).
	a.applyWindowState()

	a.scale = float64(dpiFor(hwnd)) / 96
	a.makeFonts()

	for i := range a.slots {
		s := &a.slots[i]
		s.cell = a.createChild("TCVCell", i, wsChild|wsClipChildren|wsClipSiblings)
	}
	a.loadIcons()

	created := a.applyConfig()
	a.addTray()
	if startedHidden() {
		a.logf("started minimized/hidden: the window stays in the tray")
	} else {
		a.showWindow()
	}
	a.layout()
	a.startCameras()
	if created {
		messageBox(hwnd, "No cameras.conf was found, so an example file was created here:\n\n"+a.confPath+
			"\n\nAdd one RTSP URL per line and save the file. Then right-click the tray icon and choose \"Reload cameras\".",
			appTitle, mbOK|mbIconInfo)
	}

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return 0
}

func (a *app) createChild(class string, idx int, style uintptr) uintptr {
	h, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(utf16(class))), 0, style,
		0, 0, 10, 10, a.hwnd, 0, a.hInst, 0)
	pSetWindowLongPtr.Call(h, userData, uintptr(idx))
	return h
}

func (a *app) makeFonts() {
	if a.font != 0 {
		pDeleteObject.Call(a.font)
		pDeleteObject.Call(a.fontBold)
	}
	height := -scaled(12)
	mk := func(weight int) uintptr {
		f, _, _ := pCreateFont.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0,
			uintptr(unsafe.Pointer(utf16("Segoe UI"))))
		return f
	}
	a.font, a.fontBold = mk(400), mk(700)
}

// centerWindow gives the window its start size and centers it on the work
// area of the monitor under the mouse cursor.
func centerWindow(hwnd uintptr) {
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	mon, _, _ := pMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, monitorDefaultToPrimary)
	var mi monitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
	dpi := monitorDpi(mon)
	s := float64(dpi) / 96

	r := rect{0, 0, int32(1152 * s), int32(648 * s)}
	pAdjustWindowRectExDpi.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0, 0, uintptr(dpi))
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	ww, wh := int(mi.Work.Right-mi.Work.Left), int(mi.Work.Bottom-mi.Work.Top)
	w, h = min(w, ww*9/10), min(h, wh*9/10)
	x := int(mi.Work.Left) + (ww-w)/2
	y := int(mi.Work.Top) + (wh-h)/2
	setPos(hwnd, x, y, w, h, 0)
}

func monitorDpi(mon uintptr) int {
	var dx, dy uint32
	if r, _, _ := pGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r != 0 || dx == 0 {
		return 96
	}
	return int(dx)
}

// monitorList is filled by enumMonitors. The callback is created once,
// because syscall.NewCallback memory is never freed.
var (
	monitorList []uintptr
	monitorCb   = syscall.NewCallback(func(hMonitor, hdc, lprc, data uintptr) uintptr {
		monitorList = append(monitorList, hMonitor)
		return 1 // continue
	})
)

// enumMonitors returns all monitor handles (the first one is the primary).
func enumMonitors() []uintptr {
	monitorList = monitorList[:0]
	pEnumDisplayMonitors.Call(0, 0, monitorCb, 0)
	return monitorList
}

// getMonitorByIndex returns the monitor handle for the index, or 0.
func getMonitorByIndex(index int) uintptr {
	if list := enumMonitors(); index >= 0 && index < len(list) {
		return list[index]
	}
	return 0
}

func getMonitorIndex(target uintptr) int {
	for i, m := range enumMonitors() {
		if m == target {
			return i
		}
	}
	return 0
}

// startedHidden is true when the program was started minimized or hidden
// (for example by DelayedTaskRunner). The window then stays in the tray.
func startedHidden() bool {
	var si windows.StartupInfo
	if windows.GetStartupInfo(&si) != nil || si.Flags&windows.STARTF_USESHOWWINDOW == 0 {
		return false
	}
	switch si.ShowWindow {
	case 0, 2, 6, 7, 11: // SW_HIDE, SHOWMINIMIZED, MINIMIZE, SHOWMINNOACTIVE, FORCEMINIMIZE
		return true
	}
	return false
}

// applyWindowState moves the (hidden) window to the saved place and size. The
// maximized state is applied when the window is shown (showWindow).
func (a *app) applyWindowState() {
	ws := a.set.Window
	if !ws.Valid {
		centerWindow(a.hwnd)
		return
	}
	mon := getMonitorByIndex(ws.Monitor)
	if mon == 0 { // that monitor is gone: use the one nearest to the saved place
		r := rect{int32(ws.X), int32(ws.Y), int32(ws.X + ws.Width), int32(ws.Y + ws.Height)}
		mon, _, _ = pMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	}
	var mi monitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))

	// The saved rectangle is the outer window rectangle in pixels. Keep it inside the work area.
	ww, wh := int(mi.Work.Right-mi.Work.Left), int(mi.Work.Bottom-mi.Work.Top)
	w, h := min(ws.Width, ww), min(ws.Height, wh)
	x := max(int(mi.Work.Left), min(ws.X, int(mi.Work.Right)-w))
	y := max(int(mi.Work.Top), min(ws.Y, int(mi.Work.Bottom)-h))
	setPos(a.hwnd, x, y, w, h, 0)
	a.pendingMax = ws.Maximized
}

// showWindow shows the window in its remembered state (normal or maximized).
func (a *app) showWindow() {
	if a.pendingMax {
		a.pendingMax = false
		pShowWindow.Call(a.hwnd, swMaximize)
	} else {
		pShowWindow.Call(a.hwnd, swShow) // a hidden maximized window comes back maximized
	}
	pSetForegroundWindow.Call(a.hwnd)
}

// hideWindow hides the window to the tray and remembers where it was.
func (a *app) hideWindow() {
	a.closePanel()
	a.saveWindowState()
	pShowWindow.Call(a.hwnd, swHide)
}

// saveWindowState remembers the window place, size, state and monitor.
func (a *app) saveWindowState() {
	if a.hwnd == 0 {
		return
	}
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	pGetWindowPlacement.Call(a.hwnd, uintptr(unsafe.Pointer(&wp)))
	style, _, _ := pGetWindowLongPtr.Call(a.hwnd, uintptr(gwlStyleIdx))
	maximized := a.pendingMax || style&wsMaximize != 0

	r := wp.NormalPosition // the normal rectangle, also while maximized
	if r.Right-r.Left < 200 || r.Bottom-r.Top < 120 {
		return
	}
	mon, _, _ := pMonitorFromWindow.Call(a.hwnd, monitorDefaultToNearest)
	a.set.Window = camera.WindowState{
		X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top),
		Maximized: maximized, Monitor: getMonitorIndex(mon), Valid: true,
	}
	a.saveSettings()
}

func (a *app) saveSettings() {
	if err := a.set.Save(a.dir); err != nil {
		a.logf("save settings: %v", err)
	}
}

// ---- configuration and players -------------------------------------------

// applyConfig reads cameras.conf into the slots. It returns true if an
// example file was created.
func (a *app) applyConfig() (created bool) {
	entries, created, err := camera.Load(a.confPath)
	if err != nil {
		a.logf("cannot read %s: %v", a.confPath, err)
		messageBox(a.hwnd, "Cannot read cameras.conf:\n\n"+err.Error(), appTitle, mbOK|mbIconError)
	}
	a.mu.Lock()
	for i := range a.slots {
		s := &a.slots[i]
		s.active, s.inst, s.state, s.detail = false, nil, player.Disconnected, ""
	}
	for _, e := range entries {
		s := &a.slots[e.Number-1]
		s.active, s.entry = true, e
		if e.Err != "" {
			s.state, s.detail = player.Error, e.Err
			a.logf("cam %d: %s", e.Number, e.Err)
		} else {
			s.state = player.Connecting
		}
	}
	a.mu.Unlock()
	a.full = -1
	a.logf("config: %d camera(s)", len(entries))
	return created
}

func (a *app) callbacks() player.Callbacks {
	hwnd := a.hwnd
	return player.Callbacks{
		StateChanged: func(idx int) { postMessage(hwnd, wmAppState, uintptr(idx), 0) },
		Action: func(idx int, action string) {
			l := uintptr(actBack)
			switch action {
			case "toggle":
				l = actToggle
			case "menu":
				l = actMenu
			case "click":
				l = actClick
			}
			postMessage(hwnd, wmAppAction, uintptr(idx), l)
		},
		Logf: a.logf,
	}
}

// startCameras creates the players. libmpv needs some time for each one, so
// this runs outside the UI thread.
func (a *app) startCameras() {
	cb := a.callbacks()
	for i := range a.slots {
		s := &a.slots[i]
		if !s.active || s.entry.Err != "" {
			continue
		}
		a.starting.Add(1)
		go func(idx int, url, fullURL, normalAR, fullAR string, wid uintptr, pic camera.Picture) {
			defer a.starting.Done()
			inst, err := player.New(idx, url, fullURL, normalAR, fullAR, wid, a.scale, pic.Brightness, pic.Contrast, cb)
			a.mu.Lock()
			if err != nil {
				a.slots[idx].state, a.slots[idx].detail = player.Error, err.Error()
				a.logf("cam %d: cannot start player: %v", idx+1, err)
			} else {
				a.slots[idx].inst = inst
			}
			a.mu.Unlock()
			postMessage(a.hwnd, wmAppState, uintptr(idx), 0)
		}(i, s.entry.URL, s.entry.FullURL, s.entry.NormalAR, s.entry.FullAR, s.cell, a.set.Cams[i])
	}
}

// takePlayers removes all players from the slots and returns them.
func (a *app) takePlayers() []*player.Instance {
	a.mu.Lock()
	defer a.mu.Unlock()
	var list []*player.Instance
	for i := range a.slots {
		if a.slots[i].inst != nil {
			list = append(list, a.slots[i].inst)
			a.slots[i].inst = nil
		}
	}
	return list
}

// stopPlayers stops all players in the background and then posts done to the
// main window. The UI thread must keep running its message loop meanwhile:
// libmpv closes its video windows, which are children of our windows.
func (a *app) stopPlayers(done uint32) {
	go func() {
		a.starting.Wait()
		player.CloseAll(a.takePlayers())
		postMessage(a.hwnd, done, 0, 0)
	}()
}

func (a *app) reload() {
	if a.reloading || a.closing {
		return
	}
	a.reloading = true
	a.closePanel()
	a.logf("reloading cameras")
	a.mu.Lock()
	for i := range a.slots {
		a.slots[i].state, a.slots[i].detail = player.Disconnected, "reloading"
	}
	a.mu.Unlock()
	a.stopPlayers(wmAppDone)
}

func (a *app) close() {
	if a.closing {
		return
	}
	a.closing = true
	a.closePanel()
	a.saveWindowState()
	pShowWindow.Call(a.hwnd, swHide)
	a.removeTray()
	a.stopPlayers(wmAppQuit)
}

// status returns what the badge of a slot shows.
func (a *app) status(idx int) (player.State, string) {
	a.mu.Lock()
	s := a.slots[idx]
	a.mu.Unlock()
	if s.inst != nil {
		return s.inst.Snapshot()
	}
	return s.state, s.detail
}

// ---- layout --------------------------------------------------------------

func (a *app) gap() int { return scaled(2) }

// bounds returns the position of a slot in the client area of the main window.
func (a *app) bounds(i, W, H int) (x, y, w, h int, visible bool) {
	if !a.slots[i].active {
		return
	}
	if a.full >= 0 {
		return 0, 0, W, H, i == a.full
	}
	x, y, w, h = a.gridCell(i, W, H)
	return x, y, w, h, true
}

func hide(hwnd uintptr) {
	pSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpHideWindow|swpNoZOrder|swpNoActivate|swpNoMove|swpNoSize)
}

func (a *app) layout() {
	r := clientRect(a.hwnd)
	W, H := int(r.Right), int(r.Bottom)
	if W <= 0 || H <= 0 {
		return
	}
	for i := range a.slots {
		x, y, w, h, vis := a.bounds(i, W, H)
		if !vis {
			hide(a.slots[i].cell)
			continue
		}
		setPos(a.slots[i].cell, x, y, w, h, swpShowWindow)
		// The libmpv video window is the only child of the cell: make it fill the cell.
		if child, _, _ := pGetWindow.Call(a.slots[i].cell, gwChild); child != 0 {
			setPos(child, 0, 0, w, h, 0)
		}
		a.mu.Lock()
		inst := a.slots[i].inst
		a.mu.Unlock()
		if inst != nil {
			inst.SetSize(w, h)
		}
	}
	invalidate(a.hwnd)
}

// refreshSlot redraws the status text of a slot that has no player window.
func (a *app) refreshSlot(i int) {
	if i < 0 || i >= len(a.slots) {
		return
	}
	invalidate(a.slots[i].cell)
	a.mu.Lock()
	inst := a.slots[i].inst
	a.mu.Unlock()
	if inst != nil {
		r := clientRect(a.slots[i].cell)
		inst.SetSize(int(r.Right), int(r.Bottom))
	}
}

func (a *app) toggleFull(idx int) {
	if idx < 0 || idx >= len(a.slots) || !a.slots[idx].active {
		return
	}
	a.closePanel()
	if a.full == idx {
		a.full = -1
	} else {
		if a.full >= 0 {
			a.setSlotFull(a.full, false)
		}
		a.full = idx
	}
	if a.full >= 0 {
		a.setSlotFull(a.full, true)
	} else {
		a.setSlotFull(idx, false)
	}
	a.layout()
}

// setSlotFull tells a player to use its fullscreen or its grid stream.
// The other cameras close their streams while one camera is fullscreen
// (all network bandwidth goes to that camera) and open them again in the grid.
func (a *app) setSlotFull(idx int, full bool) {
	a.mu.Lock()
	insts := make([]*player.Instance, len(a.slots))
	for i := range a.slots {
		insts[i] = a.slots[i].inst
	}
	a.mu.Unlock()
	for i, inst := range insts {
		if inst == nil {
			continue
		}
		if i == idx {
			inst.SetActive(true)
			inst.SetFull(full)
		} else {
			inst.SetActive(!full)
		}
	}
}

// ---- window procedures ---------------------------------------------------

func mainProc(hwnd, m, w, l uintptr) uintptr {
	if a == nil || a.hwnd == 0 {
		return defWindowProc(hwnd, m, w, l)
	}
	switch uint32(m) {
	case wmSize:
		if w == sizeMinimized {
			a.hideWindow() // minimize to the tray
			return 0
		}
		a.layout()
		if !a.dragging && (w == sizeMaximized || w == sizeRestored) {
			a.saveWindowState() // maximize or restore (not every step of a drag)
		}
		return 0
	case wmEnterSizeMove:
		a.dragging = true
		return 0
	case wmExitSizeMove:
		a.dragging = false
		a.saveWindowState() // the user finished moving or resizing
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		a.paintMain()
		return 0
	case wmKeyDown:
		if w == vkEscape && a.full >= 0 {
			a.toggleFull(a.full)
		}
		return 0
	case wmDpiChanged:
		a.scale = float64(loword(w)&0xffff) / 96
		a.makeFonts()
		r := (*rect)(*(*unsafe.Pointer)(unsafe.Pointer(&l)))
		setPos(hwnd, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 0)
		return 0
	case wmAppState:
		a.refreshSlot(int(w))
		a.updateTray()
		return 0
	case wmAppAction:
		if l == actMenu || (l == actClick && a.onButton(int(w))) {
			a.openPanel(int(w))
		} else if l == actClick {
			// plain click on the video: nothing
		} else if l == actToggle {
			if !a.onButton(int(w)) { // a double click on the contrast button is not "fullscreen"
				a.toggleFull(int(w))
			}
		} else if a.full >= 0 {
			a.toggleFull(a.full)
		}
		return 0
	case wmAppTray:
		switch uint32(l) {
		case wmLButtonUp:
			a.toggleWindow()
		case wmRButtonUp:
			a.trayMenu()
		}
		return 0
	case wmAppDone:
		a.reloading = false
		created := a.applyConfig()
		a.layout()
		a.startCameras()
		a.updateTray()
		if created {
			messageBox(hwnd, "cameras.conf was missing, so an example file was created:\n\n"+a.confPath, appTitle, mbOK|mbIconInfo)
		}
		return 0
	case wmAppQuit:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmClose:
		// Close button (X) minimizes to tray instead of closing
		a.hideWindow()
		return 0
	case wmDestroy:
		a.removeTray()
		pPostQuitMessage.Call(0)
		return 0
	}
	if a.taskbarCreated != 0 && uint32(m) == a.taskbarCreated {
		a.addTray() // Explorer was restarted
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

func (a *app) paintMain() {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&ps)))
	rc := clientRect(a.hwnd)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), a.bgBrush)
	W, H := int(rc.Right), int(rc.Bottom)
	haveCameras := a.full >= 0
	if a.full < 0 {
		g := a.gap()
		cw, ch := (W-(cols-1)*g)/cols, (H-(rows-1)*g)/rows
		for i := range a.slots {
			if a.slots[i].active {
				haveCameras = true
				continue
			}
			// empty grid cell: black
			x, y := (i%cols)*(cw+g), (i/cols)*(ch+g)
			e := rect{int32(x), int32(y), int32(x + cw), int32(y + ch)}
			pFillRect.Call(hdc, uintptr(unsafe.Pointer(&e)), a.blackBr)
		}
	}
	if !haveCameras {
		pSetBkMode.Call(hdc, transparentMode)
		pSetTextColor.Call(hdc, rgb(200, 200, 200))
		pSelectObject.Call(hdc, a.font)
		hint := "No cameras configured.\nEdit cameras.conf and save it. Then right-click the tray icon and choose \"Reload cameras\"."
		box := rect{int32(W / 8), int32(H/2 - scaled(40)), int32(W - W/8), int32(H/2 + scaled(60))}
		pDrawText.Call(hdc, uintptr(unsafe.Pointer(utf16(hint))), ^uintptr(0), uintptr(unsafe.Pointer(&box)),
			dtCenter|dtWordBreak|dtNoPrefix)
	}
	pEndPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&ps)))
}

// cellProc is the window of one camera. libmpv draws in a child window of it.
func cellProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmLButtonDblClk:
		idx, _, _ := pGetWindowLongPtr.Call(hwnd, userData)
		postMessage(a.hwnd, wmAppAction, idx, actToggle)
		return 0
	case wmLButtonUp, wmRButtonUp:
		// libmpv's video window passes the mouse to this cell window
		idx, _, _ := pGetWindowLongPtr.Call(hwnd, userData)
		act := uintptr(actMenu)
		if uint32(m) == wmLButtonUp {
			act = actClick
		}
		postMessage(a.hwnd, wmAppAction, idx, act)
		return 0
	case wmKeyDown:
		if w == vkEscape {
			postMessage(a.hwnd, wmAppAction, 0, actBack)
		}
		return 0
	case wmPaint:
		idx, _, _ := pGetWindowLongPtr.Call(hwnd, userData)
		a.paintCell(hwnd, int(idx))
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

// paintCell draws the camera number and the status. It is only visible while
// libmpv has no video window in the cell (no player, or the player is starting);
// the player shows the same text itself through its OSD.
func (a *app) paintCell(hwnd uintptr, idx int) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if idx < 0 || idx >= len(a.slots) {
		return
	}
	st, detail := a.status(idx)
	r, g, b := player.StateColor(st)
	br, _, _ := pCreateSolidBrush.Call(rgb(r, g, b))
	old, _, _ := pSelectObject.Call(hdc, br)
	x0, y0, d := scaled(10), scaled(10), scaled(10)
	pEllipse.Call(hdc, uintptr(x0), uintptr(y0), uintptr(x0+d), uintptr(y0+d))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(br)

	pSetBkMode.Call(hdc, transparentMode)
	pSelectObject.Call(hdc, a.fontBold)
	pSetTextColor.Call(hdc, rgb(240, 240, 240))
	left := int32(x0 + d + scaled(6))
	line := rect{left, int32(scaled(5)), left + int32(scaled(400)), int32(scaled(5) + scaled(20))}
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(utf16(fmt.Sprint(idx+1)))), ^uintptr(0), uintptr(unsafe.Pointer(&line)),
		dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	text := player.StatusText(st, detail)
	if text != "" {
		pSelectObject.Call(hdc, a.font)
		pSetTextColor.Call(hdc, rgb(200, 200, 200))
		line.Left += int32(scaled(18))
		pDrawText.Call(hdc, uintptr(unsafe.Pointer(utf16(text))), ^uintptr(0), uintptr(unsafe.Pointer(&line)),
			dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	}
}

// ownerProc is the hidden owner of the main window. It only exists so that
// the main window has no taskbar button. It ignores WM_CLOSE.
func ownerProc(hwnd, m, w, l uintptr) uintptr {
	if uint32(m) == wmClose {
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}
