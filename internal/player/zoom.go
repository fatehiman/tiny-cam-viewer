package player

import (
	"math"
	"strconv"
)

// Zoom (fullscreen only). mpv's video-zoom is log2 of the scale factor
// (0 = 1x, 1 = 2x). video-pan-x/y move the picture in units of the zoomed
// picture size. mpv places the picture at (D - S)/2 + pan*S, where D is the
// window size and S the zoomed picture size (both per axis).
const (
	zoomStep = 0.25 // one wheel notch = 2^0.25 = about x1.19
	zoomMax  = 3.0  // 2^3 = 8x
)

// picSize is the size in pixels of the picture at zoom 1x (it fits in the
// window, keepaspect). Call with i.mu held.
func (i *Instance) picSize() (float64, float64) {
	w, h := float64(i.w), float64(i.h2)
	if w <= 0 || h <= 0 || i.aspect <= 0 {
		return w, h
	}
	if w/h > i.aspect {
		return h * i.aspect, h
	}
	return w, w / i.aspect
}

// clampPan keeps the zoomed picture over the window (no empty border on a
// side that could be filled). Call with i.mu held.
func (i *Instance) clampPan() {
	pw, ph := i.picSize()
	s := math.Exp2(i.zoom)
	limit := func(pan, pic, win float64) float64 {
		S := pic * s
		m := math.Max(0, (S-win)/(2*S))
		return math.Max(-m, math.Min(m, pan))
	}
	i.panX = limit(i.panX, pw, float64(i.w))
	i.panY = limit(i.panY, ph, float64(i.h2))
}

// Zoom zooms in (steps > 0) or out (steps < 0) by mouse wheel notches.
// The picture point under (cx, cy) (pixels in the cell) stays in place.
func (i *Instance) Zoom(steps float64, cx, cy int) {
	i.mu.Lock()
	old := i.zoom
	i.zoom = math.Max(0, math.Min(zoomMax, old+steps*zoomStep))
	pw, ph := i.picSize()
	if i.closing || i.zoom == old || pw <= 0 || ph <= 0 {
		i.zoom = old
		i.mu.Unlock()
		return
	}
	s0, s1 := math.Exp2(old), math.Exp2(i.zoom)
	i.panX += (float64(cx) - float64(i.w)/2) * (1/(pw*s1) - 1/(pw*s0))
	i.panY += (float64(cy) - float64(i.h2)/2) * (1/(ph*s1) - 1/(ph*s0))
	if i.zoom == 0 {
		i.panX, i.panY = 0, 0
	}
	i.clampPan()
	st, d := i.state, i.detail
	i.mu.Unlock()
	i.applyZoom(true)
	i.updateOSD(st, d) // the status shows the zoom factor
}

// Pan moves the zoomed picture by (dx, dy) pixels (mouse drag).
func (i *Instance) Pan(dx, dy int) {
	i.mu.Lock()
	pw, ph := i.picSize()
	if i.closing || i.zoom == 0 || pw <= 0 || ph <= 0 {
		i.mu.Unlock()
		return
	}
	s := math.Exp2(i.zoom)
	i.panX += float64(dx) / (pw * s)
	i.panY += float64(dy) / (ph * s)
	i.clampPan()
	i.mu.Unlock()
	i.applyZoom(false)
}

// Zoomed tells if the picture is zoomed in.
func (i *Instance) Zoomed() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.zoom > 0
}

// ResetZoom goes back to 1x (used when fullscreen ends).
func (i *Instance) ResetZoom() {
	i.mu.Lock()
	was := i.zoom != 0 || i.panX != 0 || i.panY != 0
	i.zoom, i.panX, i.panY = 0, 0, 0
	st, d, closing := i.state, i.detail, i.closing
	i.mu.Unlock()
	if was && !closing {
		i.applyZoom(true)
		i.updateOSD(st, d)
	}
}

// applyZoom sends the zoom (only if withZoom) and the pan to libmpv.
// These are options, so they also stay after loadfile.
func (i *Instance) applyZoom(withZoom bool) {
	i.mu.Lock()
	z, x, y := i.zoom, i.panX, i.panY
	i.mu.Unlock()
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 5, 64) }
	if withZoom {
		mpvCommand(i.h, "set", "video-zoom", f(z))
	}
	mpvCommand(i.h, "set", "video-pan-x", f(x))
	mpvCommand(i.h, "set", "video-pan-y", f(y))
}

// readAspect stores the display aspect ratio of the video (for zoom math).
func (i *Instance) readAspect() {
	for _, p := range []string{"video-out-params/aspect", "video-params/aspect"} {
		if a, err := strconv.ParseFloat(mpvGetProperty(i.h, p), 64); err == nil && a > 0 {
			i.mu.Lock()
			i.aspect = a
			i.mu.Unlock()
			return
		}
	}
}
