package player

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// StateColor is the color (r, g, b) of the status dot.
func StateColor(s State) (r, g, b uint8) {
	switch s {
	case Playing:
		return 60, 192, 106
	case Connecting, Reconnecting:
		return 240, 180, 50
	case Error:
		return 225, 80, 80
	}
	return 130, 130, 130
}

// StatusText is the short text next to the camera number.
func StatusText(s State, detail string) string {
	if s == Playing {
		return detail // "HW" or "SW"
	}
	if detail == "" {
		return s.String()
	}
	return s.String() + ": " + detail
}

// assEscape makes text safe inside an ASS override block line.
func assEscape(s string) string {
	s = strings.NewReplacer(`\`, `\`, "{", "(", "}", ")", "\n", " ").Replace(s)
	return s
}

// osdLayout gives the pixel positions in the video window: padding, font size,
// the button (x, y, diameter) and the x of the status text.
func osdLayout(scale float64) (pad, size, bx, by, d, sx int) {
	pad = int(8*scale + .5)
	size = int(15*scale + .5)
	d = int(16*scale + .5)
	bx = pad + int(2.4*float64(size)+.5)
	by = pad + int(0.1*float64(size)+.5)
	sx = bx + d + int(0.5*float64(size)+.5)
	return
}

// ButtonRect is the click area of the contrast button, in pixels of the cell.
func ButtonRect(scale float64) (x, y, w, h int) {
	_, _, bx, by, d, _ := osdLayout(scale)
	m := int(3*scale + .5)
	return bx - m, by - m, d + 2*m, d + 2*m
}

// updateOSD shows the camera number, the contrast button and the status in the
// video window. A native child window can not be drawn over a Direct3D video
// window, so all of it is drawn by libmpv itself (osd-overlay).
func (i *Instance) updateOSD(s State, detail string) {
	r, g, b := StateColor(s)
	pad, size, bx, by, d, sx := osdLayout(i.scale)
	i.mu.Lock()
	w, h, zoom := i.w, i.h2, i.zoom
	i.mu.Unlock()
	text := StatusText(s, detail)
	if zoom > 0 {
		text += fmt.Sprintf("  %.1fx", math.Exp2(zoom))
	}
	text = assEscape(text)
	// state dot: a colored disc, vertically centered with the contrast icon
	dd := int(0.65*float64(d) + .5)
	dot := fmt.Sprintf(`{\an7\pos(%d,%d)\bord1\shad0\3c&H000000&\1c&H%02X%02X%02X&\p1}%s{\p0}`,
		pad, by+(d-dd)/2, b, g, r, discPath(float64(dd)/2))
	num := fmt.Sprintf(`{\an7\pos(%d,%d)\fs%d\b1\bord2\shad0\3c&H000000&\1c&HF0F0F0&}%d`,
		pad+dd+int(0.3*float64(size)+.5), pad, size, i.idx+1)
	status := fmt.Sprintf(`{\an7\pos(%d,%d)\fs%d\bord2\shad0\3c&H000000&\1c&HC8C8C8&}%s`, sx, pad, size, text)
	// contrast icon: a white disc with a dark right half
	rr := float64(d) / 2
	k := 0.5523 * rr
	half := fmt.Sprintf("m %.1f 0 b %.1f 0 %.1f %.1f %.1f %.1f b %.1f %.1f %.1f %.1f %.1f %.1f",
		rr, rr+k, 2*rr, rr-k, 2*rr, rr, 2*rr, rr+k, rr+k, 2*rr, rr, 2*rr)
	// two overlays at the same position (libass puts drawings of one event side by side)
	button := fmt.Sprintf(`{\an7\pos(%d,%d)\bord1\shad0\3c&HF0F0F0&\1c&HF0F0F0&\p1}%s{\p0}`, bx, by, discPath(rr))
	dark := fmt.Sprintf(`{\an7\pos(%d,%d)\bord1\shad0\3c&HF0F0F0&\1c&H202020&\p1}%s{\p0}`, bx, by, half)
	// res_x / res_y = real size in pixels, so that the text has a fixed size.
	// z: the dark half must be drawn over the white disc (same z = no fixed order).
	for n, ass := range []string{dot, num, button, dark, status} {
		z := "0"
		if n == 3 {
			z = "1"
		}
		mpvCommand(i.h, "osd-overlay", strconv.Itoa(n+1), "ass-events", ass, strconv.Itoa(w), strconv.Itoa(h), z)
	}
}

// discPath is an ASS drawing of a circle with radius rr (4 bezier curves).
func discPath(rr float64) string {
	k := 0.5523 * rr
	return fmt.Sprintf("m 0 %.1f b 0 %.1f %.1f 0 %.1f 0 b %.1f 0 %.1f %.1f %.1f %.1f b %.1f %.1f %.1f %.1f %.1f %.1f b %.1f %.1f 0 %.1f 0 %.1f",
		rr, rr-k, rr-k, rr, rr+k, 2*rr, rr-k, 2*rr, rr, 2*rr, rr+k, rr+k, 2*rr, rr, 2*rr, rr-k, 2*rr, rr+k, rr)
}

// SetSize tells the player how big its video window is (in pixels).
func (i *Instance) SetSize(w, h int) {
	i.mu.Lock()
	same := i.w == w && i.h2 == h
	i.w, i.h2 = w, h
	zoomed := i.zoom > 0
	if zoomed {
		i.clampPan()
	}
	s, d, closing := i.state, i.detail, i.closing
	i.mu.Unlock()
	if !same && !closing {
		if zoomed {
			i.applyZoom(false)
		}
		i.updateOSD(s, d)
	}
}
