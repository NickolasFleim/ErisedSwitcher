//go:build windows

// Package win — тонкая обёртка над Win32 API (только стандартная библиотека Go).
package win

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pSetWindowsHookExW             = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx           = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx                = user32.NewProc("CallNextHookEx")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pPostThreadMessageW            = user32.NewProc("PostThreadMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pSendInput                     = user32.NewProc("SendInput")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	pGetGUIThreadInfo              = user32.NewProc("GetGUIThreadInfo")
	pGetKeyboardLayout             = user32.NewProc("GetKeyboardLayout")
	pGetKeyboardLayoutList         = user32.NewProc("GetKeyboardLayoutList")
	pGetKeyState                   = user32.NewProc("GetKeyState")
	pGetAsyncKeyState              = user32.NewProc("GetAsyncKeyState")
	pGetClassNameW                 = user32.NewProc("GetClassNameW")
	pGetWindowTextW                = user32.NewProc("GetWindowTextW")
	pGetWindowLongPtrW             = user32.NewProc("GetWindowLongPtrW")
	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pFindWindowW                   = user32.NewProc("FindWindowW")
	pSetTimer                      = user32.NewProc("SetTimer")
	pCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                   = user32.NewProc("AppendMenuW")
	pTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                   = user32.NewProc("DestroyMenu")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pCreateIconIndirect            = user32.NewProc("CreateIconIndirect")
	pDestroyIcon                   = user32.NewProc("DestroyIcon")
	pGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	pMessageBoxW                   = user32.NewProc("MessageBoxW")
	pOpenClipboard                 = user32.NewProc("OpenClipboard")
	pCloseClipboard                = user32.NewProc("CloseClipboard")
	pEmptyClipboard                = user32.NewProc("EmptyClipboard")
	pGetClipboardData              = user32.NewProc("GetClipboardData")
	pSetClipboardData              = user32.NewProc("SetClipboardData")
	pGetClipboardSequenceNumber    = user32.NewProc("GetClipboardSequenceNumber")
	pRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pGetDoubleClickTime            = user32.NewProc("GetDoubleClickTime")
	pGetAncestor                   = user32.NewProc("GetAncestor")

	pGetCurrentThreadId            = kernel32.NewProc("GetCurrentThreadId")
	pOpenProcess                   = kernel32.NewProc("OpenProcess")
	pCloseHandle                   = kernel32.NewProc("CloseHandle")
	pQueryFullProcessImageNameW    = kernel32.NewProc("QueryFullProcessImageNameW")
	pAttachConsole                 = kernel32.NewProc("AttachConsole")
	pFreeConsole                   = kernel32.NewProc("FreeConsole")
	pGetConsoleKeyboardLayoutNameW = kernel32.NewProc("GetConsoleKeyboardLayoutNameW")
	pGlobalAlloc                   = kernel32.NewProc("GlobalAlloc")
	pGlobalFree                    = kernel32.NewProc("GlobalFree")
	pGlobalLock                    = kernel32.NewProc("GlobalLock")
	pGlobalSize                    = kernel32.NewProc("GlobalSize")
	pGlobalUnlock                  = kernel32.NewProc("GlobalUnlock")
	pCreateMutexW                  = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW              = kernel32.NewProc("GetModuleHandleW")
	pSetConsoleCtrlHandler         = kernel32.NewProc("SetConsoleCtrlHandler")

	pShell_NotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW     = shell32.NewProc("ShellExecuteW")
	pIsUserAnAdmin     = shell32.NewProc("IsUserAnAdmin")

	pCreateBitmap     = gdi32.NewProc("CreateBitmap")
	pCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	pDeleteObject     = gdi32.NewProc("DeleteObject")

	pRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

// Константы Win32.
const (
	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14

	WM_DESTROY                = 0x0002
	WM_CLOSE                  = 0x0010
	WM_QUIT                   = 0x0012
	WM_INPUTLANGCHANGEREQUEST = 0x0050
	WM_COMMAND                = 0x0111
	WM_TIMER                  = 0x0113
	WM_KEYDOWN                = 0x0100
	WM_KEYUP                  = 0x0101
	WM_SYSKEYDOWN             = 0x0104
	WM_SYSKEYUP               = 0x0105
	WM_LBUTTONDOWN            = 0x0201
	WM_LBUTTONUP              = 0x0202
	WM_LBUTTONDBLCLK          = 0x0203
	WM_RBUTTONDOWN            = 0x0204
	WM_RBUTTONUP              = 0x0205
	WM_MBUTTONDOWN            = 0x0207
	WM_XBUTTONDOWN            = 0x020B
	WM_CONTEXTMENU            = 0x007B
	WM_APP                    = 0x8000

	LLKHF_EXTENDED = 0x01
	LLKHF_INJECTED = 0x10
	LLKHF_UP       = 0x80

	INPUT_KEYBOARD        = 1
	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002
	KEYEVENTF_UNICODE     = 0x0004

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	GWL_STYLE                         = -16
	ES_PASSWORD                       = 0x0020
	GA_ROOT                           = 2

	NIM_ADD      = 0
	NIM_MODIFY   = 1
	NIM_DELETE   = 2
	NIF_MESSAGE  = 0x1
	NIF_ICON     = 0x2
	NIF_TIP      = 0x4
	NIF_INFO     = 0x10
	NIIF_INFO    = 0x1
	NIIF_WARNING = 0x2

	MF_STRING       = 0x0
	MF_SEPARATOR    = 0x800
	MF_CHECKED      = 0x8
	MF_GRAYED       = 0x1
	TPM_RETURNCMD   = 0x100
	TPM_RIGHTBUTTON = 0x2
	TPM_BOTTOMALIGN = 0x20

	SM_CXSMICON = 49

	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x2

	HKEY_CURRENT_USER = 0x80000001
	KEY_READ          = 0x20019
	KEY_SET_VALUE     = 0x0002
	REG_SZ            = 1

	ERROR_ALREADY_EXISTS = 183
)

// MSG — сообщение окна.
type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	_       uint32
}

// POINT — точка.
type POINT struct{ X, Y int32 }

// KBDLLHOOKSTRUCT — данные низкоуровневого хука клавиатуры.
type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// MSLLHOOKSTRUCT — данные хука мыши.
type MSLLHOOKSTRUCT struct {
	Pt          POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// KEYBDINPUT — часть INPUT.
type KEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// INPUT для клавиатуры. Размер должен совпадать с sizeof(INPUT): 40 байт на x64.
type INPUT struct {
	Type uint32
	_    uint32
	Ki   KEYBDINPUT
	_    [8]byte
}

// RECT — прямоугольник.
type RECT struct{ Left, Top, Right, Bottom int32 }

// GUITHREADINFO — сведения о потоке окна.
type GUITHREADINFO struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       RECT
}

// WNDCLASSEXW — класс окна.
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// GUID — идентификатор.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// NOTIFYICONDATAW — значок в трее.
type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         GUID
	HBalloonIcon     uintptr
}

// ICONINFO — для CreateIconIndirect.
type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// BITMAPINFOHEADER — заголовок DIB.
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// Проверка размеров структур, которые передаются в API по указателю.
var _ = [1]struct{}{}[unsafe.Sizeof(INPUT{})-40]
var _ = [1]struct{}{}[unsafe.Sizeof(NOTIFYICONDATAW{})-976]
var _ = [1]struct{}{}[unsafe.Sizeof(GUITHREADINFO{})-72]
var _ = [1]struct{}{}[unsafe.Sizeof(KBDLLHOOKSTRUCT{})-24]

// UTF16 — строка Windows.
func UTF16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func copyUTF16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

// GetCurrentThreadId — идентификатор потока.
func GetCurrentThreadId() uint32 {
	r, _, _ := pGetCurrentThreadId.Call()
	return uint32(r)
}

// MessageBox — окно сообщения.
func MessageBox(title, text string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(UTF16(text))), uintptr(unsafe.Pointer(UTF16(title))), uintptr(flags))
	return int(r)
}

// ShellOpen открывает файл, папку или адрес.
func ShellOpen(target string) {
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(UTF16("open"))), uintptr(unsafe.Pointer(UTF16(target))), 0, 0, 1)
}

// RunAsAdmin перезапускает программу с правами администратора.
func RunAsAdmin(exe, args string) bool {
	r, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(UTF16("runas"))), uintptr(unsafe.Pointer(UTF16(exe))),
		uintptr(unsafe.Pointer(UTF16(args))), 0, 1)
	return r > 32
}

// IsAdmin — запущены ли мы с правами администратора.
func IsAdmin() bool {
	r, _, _ := pIsUserAnAdmin.Call()
	return r != 0
}

// SingleInstance возвращает false, если программа уже запущена.
func SingleInstance(name string) bool {
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(UTF16(name))))
	if h == 0 {
		return true
	}
	if errno, ok := err.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
		pCloseHandle.Call(h) // не продлеваем жизнь чужому мьютексу
		return false
	}
	return true
}

// IgnoreConsoleCtrlC — не завершаться от Ctrl+C, пока мы на миг подключены к чужой консоли
// (это нужно, чтобы прочитать её раскладку).
func IgnoreConsoleCtrlC() { pSetConsoleCtrlHandler.Call(0, 1) }

// FindWindow ищет окно по классу.
func FindWindow(class string) uintptr {
	r, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(UTF16(class))), 0)
	return r
}

// PostMessage отправляет сообщение окну без ожидания.
func PostMessage(hwnd uintptr, msg uint32, w, l uintptr) bool {
	r, _, _ := pPostMessageW.Call(hwnd, uintptr(msg), w, l)
	return r != 0
}

// GetAsyncKeyDown — нажата ли клавиша сейчас физически.
func GetAsyncKeyDown(vk int) bool {
	r, _, _ := pGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// CapsLockOn — включён ли CapsLock.
func CapsLockOn() bool {
	r, _, _ := pGetKeyState.Call(0x14)
	return r&1 != 0
}

// EnableDPIAwareness — чёткий значок и меню на экранах с масштабом больше 100%.
func EnableDPIAwareness() {
	if pSetProcessDpiAwarenessContext.Find() == nil {
		if r, _, _ := pSetProcessDpiAwarenessContext.Call(^uintptr(3)); r != 0 { // PER_MONITOR_AWARE_V2 = -4
			return
		}
	}
	pSetProcessDPIAware.Call()
}

// DoubleClickTime — системное время двойного щелчка, мс.
func DoubleClickTime() uint32 {
	r, _, _ := pGetDoubleClickTime.Call()
	if r == 0 {
		return 500
	}
	return uint32(r)
}
