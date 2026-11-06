package window

import (
	"fmt"
	"unsafe"

	"github.com/fatehiman/tiny-cam-viewer/internal/player"
)

// loadIcons creates the window and tray icons from the embedded PNG files.
func (a *app) loadIcons() {
	dpi := dpiFor(a.hwnd)
	pick := func(metric uintptr) uintptr {
		m, _, _ := pGetSystemMetricsDpi.Call(metric, uintptr(dpi))
		size := 48
		for _, s := range []int{16, 24, 32, 48} {
			if s >= int(m) {
				size = s
				break
			}
		}
		data, err := iconFS.ReadFile(fmt.Sprintf("assets/icon%d.png", size))
		if err != nil {
			return 0
		}
		h, _, _ := pCreateIconFromRes.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
		return h
	}
	a.smallIcon = pick(49) // SM_CXSMICON
	a.bigIcon = pick(11)   // SM_CXICON
	pSendMessage.Call(a.hwnd, wmSetIcon, 0, a.smallIcon)
	pSendMessage.Call(a.hwnd, wmSetIcon, 1, a.bigIcon)
}

func (a *app) trayTip() string {
	a.mu.Lock()
	n := 0
	for i := range a.slots {
		if a.slots[i].active {
			n++
		}
	}
	a.mu.Unlock()
	if n == 0 {
		return appTitle + " - no cameras"
	}
	playing := 0
	for i := range a.slots {
		if a.slots[i].active {
			if st, _ := a.status(i); st == player.Playing {
				playing++
			}
		}
	}
	return fmt.Sprintf("%s - %d of %d playing", appTitle, playing, n)
}

func (a *app) notify(op uintptr) {
	nid := &a.nid
	nid.Size = uint32(unsafe.Sizeof(*nid))
	nid.Hwnd = a.hwnd
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmAppTray
	nid.Icon = a.smallIcon
	copy(nid.Tip[:], utf16s(a.trayTip()))
	pShellNotifyIcon.Call(op, uintptr(unsafe.Pointer(nid)))
}

func utf16s(s string) []uint16 {
	u := make([]uint16, 0, 128)
	for _, r := range s {
		if len(u) >= 126 {
			break
		}
		u = append(u, uint16(r))
	}
	return u
}

func (a *app) addTray()    { a.notify(nimAdd) }
func (a *app) updateTray() { a.notify(nimModify) }
func (a *app) removeTray() { a.notify(nimDelete) }

func (a *app) toggleWindow() {
	if vis, _, _ := pIsWindowVisible.Call(a.hwnd); vis != 0 {
		a.hideWindow()
		return
	}
	a.showWindow() // normal or maximized, as it was
}

func (a *app) trayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu)
	label := "Show window"
	if vis, _, _ := pIsWindowVisible.Call(a.hwnd); vis != 0 {
		label = "Hide window"
	}
	add := func(id uintptr, text string) {
		pAppendMenu.Call(menu, mfString, id, uintptr(unsafe.Pointer(utf16(text))))
	}
	add(cmdToggleWindow, label)
	add(cmdReload, "Reload cameras")
	add(cmdEditConf, "Edit cameras.conf")
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	add(cmdExit, "Exit")

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(a.hwnd) // needed, or the menu does not close when you click elsewhere
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	postMessage(a.hwnd, 0, 0, 0) // WM_NULL
	switch cmd {
	case cmdToggleWindow:
		a.toggleWindow()
	case cmdReload:
		a.reload()
	case cmdEditConf:
		pShellExecute.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16("notepad.exe"))),
			uintptr(unsafe.Pointer(utf16(`"`+a.confPath+`"`))), 0, swShow)
	case cmdExit:
		a.close() // Actually exit the app
	}
}
