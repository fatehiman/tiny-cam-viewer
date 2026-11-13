package player

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Software rendering (libmpv render API, type "sw").
//
// Why: with vo=gpu every camera opened its own Direct3D 11 device (and with
// hwdec its own decoder device). The NVIDIA driver keeps ~60-80 MB per device,
// so 8 cameras used ~710 MB. With vo=libmpv + the "sw" render API, mpv scales
// each frame (with the OSD) into our memory buffer, and we copy it to the cell
// window with GDI. 8 grid cameras then use ~25 MB each (measured 2026-11-13).

// mpv_render_param types (mpv/render.h).
const (
	rpInvalid    = 0
	rpAPIType    = 1
	rpBlockFrame = 12 // MPV_RENDER_PARAM_BLOCK_FOR_TARGET_TIME
	rpSwSize     = 17
	rpSwFormat   = 18
	rpSwStride   = 19
	rpSwPointer  = 20

	renderUpdateFrame = 1 // MPV_RENDER_UPDATE_FRAME
)

type renderParam struct {
	typ  int32
	_    int32
	data unsafe.Pointer
}

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	pGetDC         = user32.NewProc("GetDC")
	pReleaseDC     = user32.NewProc("ReleaseDC")
	pStretchDIBits = gdi32.NewProc("StretchDIBits")
)

// renderers maps the callback id to the player. libmpv calls updateCb from its
// own thread; it must only wake the render goroutine (no mpv calls there).
var (
	renderMu  sync.Mutex
	renderers = map[uintptr]*Instance{}
	renderID  uintptr
	updateCb  = syscall.NewCallback(func(id uintptr) uintptr {
		renderMu.Lock()
		i := renderers[id]
		renderMu.Unlock()
		if i != nil {
			select {
			case i.upd <- struct{}{}:
			default: // a wake-up is already pending
			}
		}
		return 0
	})
)

// frame is the last picture that was rendered for the cell.
type frame struct {
	mu     sync.Mutex
	w, h   int
	stride int
	buf    []byte // backing memory (with room for 64-byte alignment)
	pix    unsafe.Pointer
	ok     bool // pix holds a rendered picture of size w x h
}

// bitmapInfoHeader is BITMAPINFOHEADER (a BITMAPINFO without color table).
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// startRender creates the render context. Call after mpv_initialize and
// before the first loadfile (vo=libmpv needs the context to show video).
func (i *Instance) startRender() error {
	api := cstr("sw")
	params := []renderParam{{typ: rpAPIType, data: unsafe.Pointer(api)}, {typ: rpInvalid}}
	var ctx uintptr
	if rc := mpvRenderCreate(&ctx, i.h, &params[0]); rc < 0 {
		return fmt.Errorf("mpv_render_context_create: %s", errString(rc))
	}
	i.rctx = ctx
	i.upd = make(chan struct{}, 1)
	i.renderStop = make(chan struct{})
	i.renderDone = make(chan struct{})

	renderMu.Lock()
	renderID++
	i.rid = renderID
	renderers[i.rid] = i
	renderMu.Unlock()

	mpvRenderSetUpdateCallback(ctx, updateCb, i.rid)
	go i.renderLoop()
	return nil
}

// renderLoop renders a new picture each time libmpv says one is ready.
func (i *Instance) renderLoop() {
	defer close(i.renderDone)
	for {
		select {
		case <-i.renderStop:
			return
		case <-i.upd:
		}
		if mpvRenderUpdate(i.rctx)&renderUpdateFrame != 0 {
			i.renderFrame()
		}
	}
}

// renderFrame lets libmpv draw the current picture (video + OSD) into our
// buffer at the cell size, then copies it to the cell window.
func (i *Instance) renderFrame() {
	i.mu.Lock()
	w, h := i.w, i.h2
	i.mu.Unlock()
	if w <= 0 || h <= 0 {
		return
	}
	f := &i.frame
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.w != w || f.h != h || f.pix == nil {
		stride := (w*4 + 63) &^ 63
		f.buf = make([]byte, stride*h+64)
		off := (64 - int(uintptr(unsafe.Pointer(&f.buf[0]))&63)) & 63
		f.pix = unsafe.Pointer(&f.buf[off])
		f.w, f.h, f.stride, f.ok = w, h, stride, false
	}
	size := &[2]int32{int32(w), int32(h)}
	format := cstr("bgr0") // B, G, R, X bytes = a 32-bit Windows DIB
	stride := new(uintptr)
	*stride = uintptr(f.stride)
	block := new(int32) // 0: do not wait for the frame's display time (live view)
	params := []renderParam{
		{typ: rpSwSize, data: unsafe.Pointer(size)},
		{typ: rpSwFormat, data: unsafe.Pointer(format)},
		{typ: rpSwStride, data: unsafe.Pointer(stride)},
		{typ: rpSwPointer, data: f.pix},
		{typ: rpBlockFrame, data: unsafe.Pointer(block)},
		{typ: rpInvalid},
	}
	if rc := mpvRender(i.rctx, &params[0]); rc < 0 {
		return
	}
	f.ok = true
	hdc, _, _ := pGetDC.Call(i.wid)
	if hdc != 0 {
		f.blit(hdc)
		pReleaseDC.Call(i.wid, hdc)
	}
}

// blit copies the frame to the top-left corner of hdc. Call with f.mu held.
func (f *frame) blit(hdc uintptr) {
	bi := bitmapInfoHeader{Width: int32(f.stride / 4), Height: -int32(f.h), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	const dibRGBColors, srcCopy = 0, 0x00CC0020
	pStretchDIBits.Call(hdc, 0, 0, uintptr(f.w), uintptr(f.h), 0, 0, uintptr(f.w), uintptr(f.h),
		uintptr(f.pix), uintptr(unsafe.Pointer(&bi)), dibRGBColors, srcCopy)
}

// PaintFrame draws the last picture into hdc (WM_PAINT of the cell).
// It returns false if there is no picture yet (the window then draws the status).
func (i *Instance) PaintFrame(hdc uintptr) bool {
	f := &i.frame
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ok {
		return false
	}
	f.blit(hdc)
	return true
}

// stopRender ends the render goroutine and frees the render context. It must
// run before mpv_terminate_destroy.
func (i *Instance) stopRender() {
	if i.rctx == 0 {
		return
	}
	close(i.renderStop)
	<-i.renderDone
	mpvRenderFree(i.rctx) // no update callback after this returns
	i.rctx = 0
	renderMu.Lock()
	delete(renderers, i.rid)
	renderMu.Unlock()
}
