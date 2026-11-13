// Package player wraps libmpv (loaded at run time, no cgo) and runs one
// independent player per camera.
package player

import (
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Event ids from mpv/client.h.
const (
	evNone            = 0
	evShutdown        = 1
	evLogMessage      = 2
	evEndFile         = 7
	evClientMessage   = 16
	evPlaybackRestart = 21
)

// MPV_ERROR_LOADING_FAILED and MPV_ERROR_UNKNOWN_FORMAT. For an RTSP URL both
// mean: the camera did not answer (wrong address, wrong login, or it is off).
const (
	errLoadingFailed = -13
	errUnknownFormat = -17
)

// End file reasons.
const (
	endEOF      = 0
	endStop     = 2
	endQuit     = 3
	endError    = 4
	endRedirect = 5
)

var (
	dll *windows.LazyDLL

	pCreate         *windows.LazyProc
	pInitialize     *windows.LazyProc
	pTerminate      *windows.LazyProc
	pSetOptionStr   *windows.LazyProc
	pCommand        *windows.LazyProc
	pGetPropertyStr *windows.LazyProc
	pFree           *windows.LazyProc
	pWaitEvent      *windows.LazyProc
	pWakeup         *windows.LazyProc
	pErrorString    *windows.LazyProc
	pRequestLog     *windows.LazyProc

	pRenderCreate  *windows.LazyProc
	pRenderSetCb   *windows.LazyProc
	pRenderUpdate  *windows.LazyProc
	pRenderRender  *windows.LazyProc
	pRenderFreeCtx *windows.LazyProc
)

// Load loads libmpv from an absolute path.
func Load(path string) error {
	dll = windows.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return err
	}
	get := func(name string) *windows.LazyProc { return dll.NewProc(name) }
	pCreate = get("mpv_create")
	pInitialize = get("mpv_initialize")
	pTerminate = get("mpv_terminate_destroy")
	pSetOptionStr = get("mpv_set_option_string")
	pCommand = get("mpv_command")
	pGetPropertyStr = get("mpv_get_property_string")
	pFree = get("mpv_free")
	pWaitEvent = get("mpv_wait_event")
	pWakeup = get("mpv_wakeup")
	pErrorString = get("mpv_error_string")
	pRequestLog = get("mpv_request_log_messages")
	pRenderCreate = get("mpv_render_context_create")
	pRenderSetCb = get("mpv_render_context_set_update_callback")
	pRenderUpdate = get("mpv_render_context_update")
	pRenderRender = get("mpv_render_context_render")
	pRenderFreeCtx = get("mpv_render_context_free")
	for _, p := range []*windows.LazyProc{pCreate, pInitialize, pTerminate, pSetOptionStr,
		pCommand, pGetPropertyStr, pFree, pWaitEvent, pWakeup, pErrorString, pRequestLog,
		pRenderCreate, pRenderSetCb, pRenderUpdate, pRenderRender, pRenderFreeCtx} {
		if err := p.Find(); err != nil {
			return err
		}
	}
	return nil
}

// cstr makes a NUL-terminated copy of s.
func cstr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// cptr converts an address of C memory (not Go memory) to a pointer.
func cptr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// goString reads a C string. p must point to C memory.
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(cptr(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(cptr(p)), n))
}

func errString(code int32) string {
	r, _, _ := pErrorString.Call(uintptr(code))
	return goString(r)
}

func mpvCreate() uintptr {
	r, _, _ := pCreate.Call()
	return r
}

func mpvInitialize(h uintptr) int32 {
	r, _, _ := pInitialize.Call(h)
	return int32(r)
}

func mpvSetOption(h uintptr, name, value string) int32 {
	n, v := cstr(name), cstr(value)
	r, _, _ := pSetOptionStr.Call(h, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(v)))
	return int32(r)
}

func mpvCommand(h uintptr, args ...string) int32 {
	arr := make([]*byte, len(args)+1) // last element stays nil
	for i, a := range args {
		arr[i] = cstr(a)
	}
	r, _, _ := pCommand.Call(h, uintptr(unsafe.Pointer(&arr[0])))
	return int32(r)
}

func mpvGetProperty(h uintptr, name string) string {
	n := cstr(name)
	r, _, _ := pGetPropertyStr.Call(h, uintptr(unsafe.Pointer(n)))
	if r == 0 {
		return ""
	}
	s := goString(r)
	pFree.Call(r)
	return s
}

func mpvWakeup(h uintptr) { pWakeup.Call(h) }

func mpvDestroy(h uintptr) { pTerminate.Call(h) }

func mpvRequestLog(h uintptr, level string) {
	pRequestLog.Call(h, uintptr(unsafe.Pointer(cstr(level))))
}

func mpvRenderCreate(ctx *uintptr, h uintptr, params *renderParam) int32 {
	r, _, _ := pRenderCreate.Call(uintptr(unsafe.Pointer(ctx)), h, uintptr(unsafe.Pointer(params)))
	return int32(r)
}

func mpvRenderSetUpdateCallback(ctx, cb, data uintptr) { pRenderSetCb.Call(ctx, cb, data) }

func mpvRenderUpdate(ctx uintptr) uint64 {
	r, _, _ := pRenderUpdate.Call(ctx)
	return uint64(r)
}

func mpvRender(ctx uintptr, params *renderParam) int32 {
	r, _, _ := pRenderRender.Call(ctx, uintptr(unsafe.Pointer(params)))
	return int32(r)
}

func mpvRenderFree(ctx uintptr) { pRenderFreeCtx.Call(ctx) }

// event is a copy of the fields we need from mpv_event.
type event struct {
	id   int32
	err  int32
	data uintptr
}

// waitEvent blocks until libmpv has an event (or the timeout in seconds).
func waitEvent(h uintptr, timeout float64) event {
	r, _, _ := pWaitEvent.Call(h, uintptr(math.Float64bits(timeout)))
	if r == 0 {
		return event{}
	}
	p := cptr(r)
	return event{
		id:   *(*int32)(p),
		err:  *(*int32)(unsafe.Add(p, 4)),
		data: *(*uintptr)(unsafe.Add(p, 16)),
	}
}

func (e event) String() string { return fmt.Sprintf("event(%d)", e.id) }

// readInt32 reads an int32 at data+off.
func readInt32(data uintptr, off uintptr) int32 {
	return *(*int32)(unsafe.Add(cptr(data), off))
}

func readPtr(data uintptr, off uintptr) uintptr {
	return *(*uintptr)(unsafe.Add(cptr(data), off))
}
