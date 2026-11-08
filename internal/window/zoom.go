package window

import (
	"unsafe"

	"github.com/fatehiman/tiny-cam-viewer/internal/player"
)

// Zoom works only in fullscreen: the mouse wheel zooms at the cursor, and
// dragging with the left button moves the zoomed picture.

// panDrag is a running left-button drag over the zoomed fullscreen camera.
type panDrag struct {
	on, moved bool
	x, y      int32 // last mouse position (cell pixels)
	dist      int32 // total movement, to tell a drag from a click
}

// fullPlayer returns the player of the fullscreen camera, or nil.
func (a *app) fullPlayer() *player.Instance {
	if a.full < 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.slots[a.full].inst
}

// zoomAt zooms the fullscreen camera idx by steps wheel notches at the cursor.
func (a *app) zoomAt(idx int, steps float64) {
	if idx < 0 || idx != a.full {
		return
	}
	inst := a.fullPlayer()
	if inst == nil {
		return
	}
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pScreenToClient.Call(a.slots[idx].cell, uintptr(unsafe.Pointer(&pt)))
	inst.Zoom(steps, int(pt.X), int(pt.Y))
}

// mousePos reads the client position from the lParam of a mouse message.
func mousePos(l uintptr) (int32, int32) {
	return int32(int16(l & 0xffff)), int32(int16((l >> 16) & 0xffff))
}

func (a *app) startDrag(cell, l uintptr) {
	inst := a.fullPlayer()
	if inst == nil || cell != a.slots[a.full].cell || !inst.Zoomed() {
		return
	}
	x, y := mousePos(l)
	a.drag = panDrag{on: true, x: x, y: y}
	pSetCapture.Call(cell)
}

func (a *app) moveDrag(l uintptr) {
	if !a.drag.on {
		return
	}
	x, y := mousePos(l)
	dx, dy := x-a.drag.x, y-a.drag.y
	if dx == 0 && dy == 0 {
		return
	}
	a.drag.x, a.drag.y = x, y
	a.drag.dist += max(dx, -dx) + max(dy, -dy)
	if a.drag.dist > 4 {
		a.drag.moved = true
	}
	if inst := a.fullPlayer(); inst != nil {
		inst.Pan(int(dx), int(dy))
	}
}

func (a *app) endDrag() {
	if a.drag.on {
		a.drag.on = false
		pReleaseCapture.Call()
	}
}
