//go:build windows

package win

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	pShowWindow          = user32.NewProc("ShowWindow")
	pIsIconic            = user32.NewProc("IsIconic")
	pSendMessageW        = user32.NewProc("SendMessageW")
	pSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW     = user32.NewProc("CallWindowProcW")
	pGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	pAllowSetForegroundW = user32.NewProc("AllowSetForegroundWindow")
)

// FindWindowByTitle ищет окно верхнего уровня по классу и заголовку.
func FindWindowByTitle(class, title string) uintptr {
	r, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(UTF16(class))), uintptr(unsafe.Pointer(UTF16(title))))
	return r
}

// BringToFront разворачивает окно (если свёрнуто) и выводит на передний план.
func BringToFront(hwnd uintptr) {
	if r, _, _ := pIsIconic.Call(hwnd); r != 0 {
		pShowWindow.Call(hwnd, 9) // SW_RESTORE
	} else {
		pShowWindow.Call(hwnd, 5) // SW_SHOW
	}
	pSetForegroundWindow.Call(hwnd)
}

// AllowForeground разрешает процессу pid вывести своё окно на передний план.
func AllowForeground(pid int) { pAllowSetForegroundW.Call(uintptr(pid)) }

// SetWindowIcon задаёт значки окна (большой — для Alt+Tab, маленький — для заголовка).
func SetWindowIcon(hwnd, big, small uintptr) {
	const WM_SETICON = 0x0080
	pSendMessageW.Call(hwnd, WM_SETICON, 1, big)
	pSendMessageW.Call(hwnd, WM_SETICON, 0, small)
}

// SystemDPI — масштаб экрана (96 = 100%).
func SystemDPI() int {
	if pGetDpiForSystem.Find() == nil {
		if r, _, _ := pGetDpiForSystem.Call(); r != 0 {
			return int(r)
		}
	}
	return 96
}

// Subclass подменяет обработчик сообщений окна. proc получает старый обработчик через next.
func Subclass(hwnd uintptr, proc func(hwnd uintptr, msg uint32, w, l uintptr, next func() uintptr) uintptr) {
	var old uintptr
	cb := syscall.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		return proc(h, msg, w, l, func() uintptr {
			r, _, _ := pCallWindowProcW.Call(old, h, uintptr(msg), w, l)
			return r
		})
	})
	old, _, _ = pSetWindowLongPtrW.Call(hwnd, ^uintptr(3), cb) // GWLP_WNDPROC = -4
}

// MessageBoxOwned — окно сообщения поверх окна owner. Возвращает нажатую кнопку.
func MessageBoxOwned(owner uintptr, title, text string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(UTF16(text))), uintptr(unsafe.Pointer(UTF16(title))), uintptr(flags))
	return int(r)
}

// ShellRun запускает программу с аргументами. Возвращает false, если не удалось.
func ShellRun(file, args string) bool {
	r, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(UTF16("open"))), uintptr(unsafe.Pointer(UTF16(file))),
		uintptr(unsafe.Pointer(UTF16(args))), 0, 1)
	return r > 32
}

var (
	pGetLastInputInfo    = user32.NewProc("GetLastInputInfo")
	pGetTickCount        = kernel32.NewProc("GetTickCount")
	pWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
)

// IdleFor — сколько времени не было нажатий и движений мыши.
func IdleFor() time.Duration {
	lii := struct{ Size, Time uint32 }{8, 0}
	if r, _, _ := pGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii))); r == 0 {
		return 0
	}
	now, _, _ := pGetTickCount.Call()
	return time.Duration(uint32(now)-lii.Time) * time.Millisecond
}

// WaitProcessExit ждёт завершения процесса pid (не дольше timeout). true — завершился.
func WaitProcessExit(pid int, timeout time.Duration) bool {
	const synchronize = 0x00100000
	h, _, _ := pOpenProcess.Call(synchronize, 0, uintptr(pid))
	if h == 0 {
		return true // процесса уже нет
	}
	defer pCloseHandle.Call(h)
	r, _, _ := pWaitForSingleObject.Call(h, uintptr(timeout.Milliseconds()))
	return r == 0 // WAIT_OBJECT_0
}

var pRtlGetVersion = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")

// WindowsVersion — например «Windows 11 (10.0.22631)».
func WindowsVersion() string {
	var vi struct {
		Size, Major, Minor, Build, Platform uint32
		CSD                                 [128]uint16
	}
	vi.Size = uint32(unsafe.Sizeof(vi))
	if r, _, _ := pRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi))); r != 0 {
		return "Windows"
	}
	name := "Windows 10"
	if vi.Major == 10 && vi.Build >= 22000 {
		name = "Windows 11"
	} else if vi.Major < 10 {
		name = "Windows"
	}
	return name + " (" + itoa(int(vi.Major)) + "." + itoa(int(vi.Minor)) + "." + itoa(int(vi.Build)) + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
