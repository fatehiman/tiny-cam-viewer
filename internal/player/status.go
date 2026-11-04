package player

import (
	"fmt"
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

// updateOSD shows the camera number and the status in the video window.
// A native child window can not be drawn over a Direct3D video window, so the
// text is drawn by libmpv itself (osd-overlay).
func (i *Instance) updateOSD(s State, detail string) {
	r, g, b := StateColor(s)
	size := int(15*i.scale + .5)
	pad := int(8*i.scale + .5)
	text := assEscape(StatusText(s, detail))
	ass := fmt.Sprintf(`{\an7\pos(%d,%d)\fs%d\bord2\shad0\3c&H000000&\1c&H%02X%02X%02X&}●{\1c&HF0F0F0&\b1} %d{\b0\1c&HC8C8C8&}  %s`,
		pad, pad, size, b, g, r, i.idx+1, text)
	i.mu.Lock()
	w, h := i.w, i.h2
	i.mu.Unlock()
	// res_x / res_y = real size in pixels, so that the text has a fixed size.
	mpvCommand(i.h, "osd-overlay", "1", "ass-events", ass, strconv.Itoa(w), strconv.Itoa(h))
}

// SetSize tells the player how big its video window is (in pixels).
func (i *Instance) SetSize(w, h int) {
	i.mu.Lock()
	same := i.w == w && i.h2 == h
	i.w, i.h2 = w, h
	s, d, closing := i.state, i.detail, i.closing
	i.mu.Unlock()
	if !same && !closing {
		i.updateOSD(s, d)
		// Notify mpv of the window size change so it can resize its video window
		mpvCommand(i.h, "set", "geometry", fmt.Sprintf("%dx%d", w, h))
	}
}
