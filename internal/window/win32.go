package window

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassEx       = user32.NewProc("RegisterClassExW")
	pCreateWindowEx        = user32.NewProc("CreateWindowExW")
	pDefWindowProc         = user32.NewProc("DefWindowProcW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pGetMessage            = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessage       = user32.NewProc("DispatchMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pPostMessage           = user32.NewProc("PostMessageW")
	pShowWindow            = user32.NewProc("ShowWindow")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pGetClientRect         = user32.NewProc("GetClientRect")
	pGetWindowRect         = user32.NewProc("GetWindowRect")
	pInvalidateRect        = user32.NewProc("InvalidateRect")
	pBeginPaint            = user32.NewProc("BeginPaint")
	pEndPaint              = user32.NewProc("EndPaint")
	pFillRect              = user32.NewProc("FillRect")
	pDrawText              = user32.NewProc("DrawTextW")
	pLoadCursor            = user32.NewProc("LoadCursorW")
	pMessageBox            = user32.NewProc("MessageBoxW")
	pSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	pMonitorFromPoint      = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfo        = user32.NewProc("GetMonitorInfoW")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pAdjustWindowRectExDpi = user32.NewProc("AdjustWindowRectExForDpi")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenu            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pIsWindowVisible       = user32.NewProc("IsWindowVisible")
	pSendMessage           = user32.NewProc("SendMessageW")
	pSetWindowLongPtr      = user32.NewProc("SetWindowLongPtrW")
	pGetWindowLongPtr      = user32.NewProc("GetWindowLongPtrW")
	pCreateIconFromRes     = user32.NewProc("CreateIconFromResourceEx")
	pDestroyIcon           = user32.NewProc("DestroyIcon")
	pRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	pGetSystemMetricsDpi   = user32.NewProc("GetSystemMetricsForDpi")
	pSetFocus              = user32.NewProc("SetFocus")
	pIsIconic              = user32.NewProc("IsIconic")

	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pCreateFont       = gdi32.NewProc("CreateFontW")
	pEllipse          = gdi32.NewProc("Ellipse")
	pGetStockObject   = gdi32.NewProc("GetStockObject")

	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	pShellExecute    = shell32.NewProc("ShellExecuteW")

	pGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	pGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")
)

// Window messages and constants.
const (
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmEraseBkgnd    = 0x0014
	wmPaint         = 0x000F
	wmGetMinMaxInfo = 0x0024
	wmSetIcon       = 0x0080
	wmKeyDown       = 0x0100
	wmCommand       = 0x0111
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmDpiChanged    = 0x02E0
	wmApp           = 0x8000

	wmAppState  = wmApp + 1 // wParam = camera slot
	wmAppAction = wmApp + 2 // wParam = camera slot, lParam = 1 toggle, 2 back
	wmAppTray   = wmApp + 3 // lParam = mouse message

	sizeMinimized = 1

	wsOverlappedWindow = 0x00CF0000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsClipSiblings     = 0x04000000
	wsClipChildren     = 0x02000000
	wsExNoParentNotify = 0x00000004

	csDblClks = 0x0008

	swHide    = 0
	swShow    = 5
	swRestore = 9

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
	swpHideWindow = 0x0080

	vkEscape = 0x1B

	dtLeft          = 0x0000
	dtCenter        = 0x0001
	dtVCenter       = 0x0004
	dtSingleLine    = 0x0020
	dtWordBreak     = 0x0010
	dtEndEllipsis   = 0x8000
	dtNoPrefix      = 0x0800
	transparentMode = 1

	idcArrow = 32512

	mbOK        = 0x0
	mbIconError = 0x10
	mbIconInfo  = 0x40
	mbTopmost   = 0x40000

	gwlpUserData = -21

	monitorDefaultToPrimary = 1

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString       = 0x0
	mfSeparator    = 0x800
	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100

	nullBrush    = 5
	cwUseDefault = 0x80000000
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type notifyIconData struct {
	Size            uint32
	Hwnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     uintptr
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func rgb(r, g, b uint8) uintptr { return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16) }

func hiword(v uintptr) int { return int(int16(v >> 16)) }
func loword(v uintptr) int { return int(int16(v)) }

func defWindowProc(hwnd, m, w, l uintptr) uintptr {
	r, _, _ := pDefWindowProc.Call(hwnd, m, w, l)
	return r
}

func postMessage(hwnd uintptr, m uint32, w, l uintptr) {
	pPostMessage.Call(hwnd, uintptr(m), w, l)
}

func messageBox(owner uintptr, text, title string, flags uint32) {
	pMessageBox.Call(owner, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), uintptr(flags))
}

// MessageBox shows a message box. It works before any window exists.
func MessageBox(text, title string, error bool) {
	f := uint32(mbOK | mbTopmost)
	if error {
		f |= mbIconError
	} else {
		f |= mbIconInfo
	}
	messageBox(0, text, title, f)
}

func clientRect(hwnd uintptr) rect {
	var r rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func invalidate(hwnd uintptr) {
	pInvalidateRect.Call(hwnd, 0, 1)
}

func dpiFor(hwnd uintptr) int {
	d, _, _ := pGetDpiForWindow.Call(hwnd)
	if d == 0 {
		return 96
	}
	return int(d)
}

func setPos(hwnd uintptr, x, y, w, h int, flags uintptr) {
	pSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), flags|swpNoZOrder|swpNoActivate)
}
