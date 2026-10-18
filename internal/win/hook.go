//go:build windows

package win

import (
	"errors"
	"syscall"
)

// KeyHookEvent — событие из низкоуровневого хука клавиатуры.
type KeyHookEvent struct {
	VK       uint16
	Scan     uint16
	Flags    uint32
	Time     uint32
	Extra    uintptr
	Up       bool
	Injected bool
}

// Hooks — установленные хуки. Обработчики вызываются в потоке хуков и должны быть быстрыми.
type Hooks struct {
	OnKey func(ev KeyHookEvent) (swallow bool)
	// OnMouse получает нажатия и отпускания кнопок мыши (msg — WM_LBUTTONDOWN и т.п.).
	OnMouse func(msg uint32, pt POINT, time uint32)

	kb, ms uintptr
}

var active *Hooks

var (
	kbProc = syscall.NewCallback(func(code int, wparam uintptr, lparam uintptr) uintptr {
		if code == 0 && active != nil && active.OnKey != nil {
			k := (*KBDLLHOOKSTRUCT)(uptr(lparam))
			ev := KeyHookEvent{
				VK: uint16(k.VkCode), Scan: uint16(k.ScanCode), Flags: k.Flags, Time: k.Time, Extra: k.DwExtraInfo,
				Up:       wparam == WM_KEYUP || wparam == WM_SYSKEYUP,
				Injected: k.Flags&LLKHF_INJECTED != 0,
			}
			if active.OnKey(ev) {
				return 1
			}
		}
		r, _, _ := pCallNextHookEx.Call(0, uintptr(code), wparam, lparam)
		return r
	})
	msProc = syscall.NewCallback(func(code int, wparam uintptr, lparam uintptr) uintptr {
		if code == 0 && active != nil && active.OnMouse != nil {
			switch wparam {
			case WM_LBUTTONDOWN, WM_LBUTTONUP, WM_RBUTTONDOWN, WM_MBUTTONDOWN, WM_XBUTTONDOWN:
				m := (*MSLLHOOKSTRUCT)(uptr(lparam))
				active.OnMouse(uint32(wparam), m.Pt, m.Time)
			}
		}
		r, _, _ := pCallNextHookEx.Call(0, uintptr(code), wparam, lparam)
		return r
	})
)

// Install ставит хуки. Вызывать из потока, в котором крутится MessageLoop
// (runtime.LockOSThread), иначе Windows не будет вызывать обработчики.
func (h *Hooks) Install() error {
	hinst, _, _ := pGetModuleHandleW.Call(0)
	active = h
	kb, _, err := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, kbProc, hinst, 0)
	if kb == 0 {
		return errors.New("не удалось установить перехват клавиатуры: " + err.Error())
	}
	h.kb = kb
	ms, _, _ := pSetWindowsHookExW.Call(WH_MOUSE_LL, msProc, hinst, 0)
	h.ms = ms // без мыши можно жить: просто буфер не сбросится по щелчку
	return nil
}

// Uninstall снимает хуки.
func (h *Hooks) Uninstall() {
	if h.kb != 0 {
		pUnhookWindowsHookEx.Call(h.kb)
		h.kb = 0
	}
	if h.ms != 0 {
		pUnhookWindowsHookEx.Call(h.ms)
		h.ms = 0
	}
}
