//go:build windows

package win

import (
	"syscall"
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
