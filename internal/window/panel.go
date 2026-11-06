package window

import (
	"fmt"
	"unsafe"

	"github.com/fatehiman/tiny-cam-viewer/internal/player"
)

// The picture panel: a small tool window with two sliders (brightness and
// contrast) and a Reset button. It opens on right-click on a camera, over the
// neighbour cell, so the camera that is being adjusted stays visible.

const idReset = 101

const (
	panelW    = 300 // client size at 96 DPI
	panelH    = 168
	sliderMax = 200 // slider position = value + 100
)

var panelLabels = [2]string{"Brightness", "Contrast"}

// panel is the state of the (one) open panel. hwnd == 0 means closed.
type panel struct {
	hwnd   uintptr
	cam    int        // camera slot index
	slider [2]uintptr // 0 = brightness, 1 = contrast
	label  [2]uintptr
}

func initControls() {
	type icc struct{ Size, ICC uint32 }
	c := icc{8, iccBarClasses}
	pInitCommonControls.Call(uintptr(unsafe.Pointer(&c)))
}

// gridCell returns the cell rectangle of slot i in the 4x2 grid (client coordinates).
func (a *app) gridCell(i, W, H int) (x, y, w, h int) {
	g := a.gap()
	w, h = (W-(cols-1)*g)/cols, (H-(rows-1)*g)/rows
	return (i % cols) * (w + g), (i / cols) * (h + g), w, h
}

// onButton tells if the mouse is on the contrast button of camera idx.
func (a *app) onButton(idx int) bool {
	if idx < 0 || idx >= len(a.slots) {
		return false
	}
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pScreenToClient.Call(a.slots[idx].cell, uintptr(unsafe.Pointer(&pt)))
	x, y, w, h := player.ButtonRect(a.scale)
	return int(pt.X) >= x && int(pt.X) < x+w && int(pt.Y) >= y && int(pt.Y) < y+h
}

func (a *app) closePanel() {
	if a.panel.hwnd != 0 {
		pDestroyWindow.Call(a.panel.hwnd)
	}
}

// openPanel opens the panel for camera slot idx.
func (a *app) openPanel(idx int) {
	if idx < 0 || idx >= len(a.slots) || !a.slots[idx].active {
		return
	}
	a.closePanel()

	const style = wsPopup | wsCaption | wsSysMenu
	dpi := dpiFor(a.hwnd)
	r := rect{0, 0, int32(scaled(panelW)), int32(scaled(panelH))}
	pAdjustWindowRectExDpi.Call(uintptr(unsafe.Pointer(&r)), style, 0, wsExToolWindow, uintptr(dpi))
	pw, ph := int(r.Right-r.Left), int(r.Bottom-r.Top)
	x, y := a.panelPos(idx, pw, ph)

	title := fmt.Sprintf("Camera %d - picture", idx+1)
	hwnd, _, _ := pCreateWindowEx.Call(wsExToolWindow, uintptr(unsafe.Pointer(utf16("TCVPanel"))),
		uintptr(unsafe.Pointer(utf16(title))), style, uintptr(x), uintptr(y), uintptr(pw), uintptr(ph),
		a.hwnd, 0, a.hInst, 0)
	if hwnd == 0 {
		return
	}
	p := &a.panel
	*p = panel{hwnd: hwnd, cam: idx}

	child := func(class, text string, style uintptr, x, y, w, h int, id uintptr) uintptr {
		c, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))),
			style|wsChild|wsVisible, uintptr(scaled(x)), uintptr(scaled(y)), uintptr(scaled(w)), uintptr(scaled(h)),
			hwnd, id, a.hInst, 0)
		pSendMessage.Call(c, wmSetFont, a.font, 1)
		return c
	}
	pic := a.set.Cams[idx]
	vals := [2]int{pic.Brightness, pic.Contrast}
	for k := 0; k < 2; k++ {
		top := 10 + k*56
		p.label[k] = child("STATIC", "", 0, 12, top, 276, 18, 0)
		p.slider[k] = child("msctls_trackbar32", "", tbsNoTicks|wsTabStop, 8, top+20, 284, 28, 0)
		pSendMessage.Call(p.slider[k], tbmSetRange, 0, uintptr(sliderMax)<<16)
		pSendMessage.Call(p.slider[k], tbmSetPageSize, 0, 10)
		pSendMessage.Call(p.slider[k], tbmSetLineSize, 0, 1)
		pSendMessage.Call(p.slider[k], tbmSetPos, 1, uintptr(vals[k]+100))
		a.panelLabel(k, vals[k])
	}
	child("BUTTON", "Reset", wsTabStop, 12, 124, 276, 30, idReset)

	pShowWindow.Call(hwnd, swShow)
	pSetForegroundWindow.Call(hwnd)
}

func (a *app) panelLabel(k, v int) {
	pSetWindowText.Call(a.panel.label[k], uintptr(unsafe.Pointer(utf16(fmt.Sprintf("%s: %+d", panelLabels[k], v)))))
}

// panelPos chooses the top-left screen position of the panel: centered on the
// neighbour cell of camera idx. In fullscreen it is in the top right corner.
func (a *app) panelPos(idx, pw, ph int) (int, int) {
	cr := clientRect(a.hwnd)
	W, H := int(cr.Right), int(cr.Bottom)
	var cx, cy int
	if a.full >= 0 {
		cx, cy = W-pw-scaled(12), scaled(12)
	} else {
		n := idx + 1
		if idx%cols == cols-1 {
			n = idx - 1
		}
		x, y, w, h := a.gridCell(n, W, H)
		cx, cy = x+(w-pw)/2, y+(h-ph)/2
	}
	pt := point{int32(cx), int32(cy)}
	pClientToScreen.Call(a.hwnd, uintptr(unsafe.Pointer(&pt)))

	// keep the panel on the screen
	mon, _, _ := pMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, monitorDefaultToNearest)
	var mi monitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
	x := max(int(mi.Work.Left), min(int(pt.X), int(mi.Work.Right)-pw))
	y := max(int(mi.Work.Top), min(int(pt.Y), int(mi.Work.Bottom)-ph))
	return x, y
}

// setPicture applies and remembers brightness and contrast of camera idx.
func (a *app) setPicture(idx, brightness, contrast int, save bool) {
	a.set.Cams[idx].Brightness, a.set.Cams[idx].Contrast = brightness, contrast
	a.mu.Lock()
	inst := a.slots[idx].inst
	a.mu.Unlock()
	if inst != nil {
		inst.SetPicture(brightness, contrast)
	}
	if save {
		a.saveSettings()
	}
}

func (a *app) sliderValue(k int) int {
	pos, _, _ := pSendMessage.Call(a.panel.slider[k], tbmGetPos, 0, 0)
	return int(pos) - 100
}

func panelProc(hwnd, m, w, l uintptr) uintptr {
	if a == nil || hwnd != a.panel.hwnd {
		return defWindowProc(hwnd, m, w, l)
	}
	p := &a.panel
	switch uint32(m) {
	case wmHScroll:
		b, c := a.sliderValue(0), a.sliderValue(1)
		a.panelLabel(0, b)
		a.panelLabel(1, c)
		// do not write the file for every step while the thumb is dragged
		a.setPicture(p.cam, b, c, uint32(loword(w)&0xffff) != tbThumbTrack)
		return 0
	case wmCommand:
		if loword(w)&0xffff == idReset {
			for k := 0; k < 2; k++ {
				pSendMessage.Call(p.slider[k], tbmSetPos, 1, 100)
				a.panelLabel(k, 0)
			}
			a.setPicture(p.cam, 0, 0, true)
		}
		return 0
	case wmActivate:
		if loword(w)&0xffff == 0 { // WA_INACTIVE: click somewhere else closes the panel
			postMessage(hwnd, wmClose, 0, 0)
		}
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		*p = panel{}
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}
