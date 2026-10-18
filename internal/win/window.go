//go:build windows

package win

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"erised/internal/keymap"
)

// WindowInfo — сведения об окне, в которое идёт ввод.
type WindowInfo struct {
	Hwnd     uintptr // окно верхнего уровня
	Focus    uintptr // окно с фокусом (может быть 0)
	Thread   uint32  // поток окна с фокусом
	PID      uint32
	Process  string // имя exe в нижнем регистре
	Class    string
	Title    string
	Password bool
}

// ForegroundWindow возвращает активное окно.
func ForegroundWindow() uintptr {
	r, _, _ := pGetForegroundWindow.Call()
	return r
}

// WindowThreadProcess — поток и процесс окна.
func WindowThreadProcess(hwnd uintptr) (tid, pid uint32) {
	r, _, _ := pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return uint32(r), pid
}

// ClassName — класс окна.
func ClassName(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

// WindowText — заголовок окна.
func WindowText(hwnd uintptr) string {
	var buf [512]uint16
	n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 512)
	return syscall.UTF16ToString(buf[:n])
}

// ProcessName — имя exe процесса.
func ProcessName(pid uint32) string {
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	var buf [1024]uint16
	n := uint32(len(buf))
	r, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:n])))
}

// GUIThreadInfo — окно с фокусом в потоке tid.
func GUIThreadInfo(tid uint32) (GUITHREADINFO, bool) {
	gi := GUITHREADINFO{CbSize: uint32(unsafe.Sizeof(GUITHREADINFO{}))}
	r, _, _ := pGetGUIThreadInfo.Call(uintptr(tid), uintptr(unsafe.Pointer(&gi)))
	return gi, r != 0
}

// Кэш сведений о процессе для окна: OpenProcess на каждое нажатие — лишняя работа.
type procCacheEntry struct {
	pid  uint32
	name string
}

var procCache = map[uintptr]procCacheEntry{}

// Describe собирает сведения об активном окне. withTitle — читать заголовок
// (нужен, только если в правилах используется заголовок).
func Describe(withTitle bool) WindowInfo {
	var w WindowInfo
	w.Hwnd = ForegroundWindow()
	if w.Hwnd == 0 {
		return w
	}
	tid, pid := WindowThreadProcess(w.Hwnd)
	w.Thread, w.PID = tid, pid
	if gi, ok := GUIThreadInfo(tid); ok && gi.HwndFocus != 0 {
		w.Focus = gi.HwndFocus
		if ftid, _ := WindowThreadProcess(gi.HwndFocus); ftid != 0 {
			w.Thread = ftid
		}
	}
	if e, ok := procCache[w.Hwnd]; ok && e.pid == pid {
		w.Process = e.name
	} else {
		w.Process = ProcessName(pid)
		if len(procCache) > 500 {
			procCache = map[uintptr]procCacheEntry{}
		}
		procCache[w.Hwnd] = procCacheEntry{pid, w.Process}
	}
	w.Class = ClassName(w.Hwnd)
	if withTitle {
		w.Title = WindowText(w.Hwnd)
	}
	if w.Focus != 0 {
		style, _, _ := pGetWindowLongPtrW.Call(w.Focus, uintptr(^uintptr(15))) // GWL_STYLE = -16
		if style&ES_PASSWORD != 0 && strings.EqualFold(ClassName(w.Focus), "Edit") {
			w.Password = true
		}
	}
	return w
}

// IsConsole — классическое окно консоли (conhost).
func (w *WindowInfo) IsConsole() bool { return w.Class == "ConsoleWindowClass" }

// ---- Раскладки ----

// HKL — идентификатор раскладки.
type HKL uintptr

// LangOf — язык раскладки (по младшему слову HKL).
func LangOf(h HKL) keymap.Lang {
	switch uint16(h) & 0x3FF {
	case 0x19:
		return keymap.RU
	case 0x09:
		return keymap.EN
	}
	return keymap.Unknown
}

// KeyboardLayout — раскладка потока.
func KeyboardLayout(tid uint32) HKL {
	r, _, _ := pGetKeyboardLayout.Call(uintptr(tid))
	return HKL(r)
}

// LayoutList — установленные раскладки.
func LayoutList() []HKL {
	n, _, _ := pGetKeyboardLayoutList.Call(0, 0)
	if n == 0 {
		return nil
	}
	buf := make([]HKL, n)
	n, _, _ = pGetKeyboardLayoutList.Call(n, uintptr(unsafe.Pointer(&buf[0])))
	return buf[:n]
}

// FindLayout ищет установленную раскладку для языка. want — явное значение из настроек
// (шестнадцатеричное HKL или KLID, например "00000419" или "04190419").
func FindLayout(l keymap.Lang, want string) HKL {
	list := LayoutList()
	if want = strings.TrimSpace(want); want != "" {
		var v uint64
		for _, c := range strings.ToLower(want) {
			switch {
			case c >= '0' && c <= '9':
				v = v<<4 | uint64(c-'0')
			case c >= 'a' && c <= 'f':
				v = v<<4 | uint64(c-'a'+10)
			}
		}
		for _, h := range list {
			if uint64(h) == v || uint64(h)&0xFFFFFFFF == v {
				return h
			}
		}
		// KLID вида 00000419 — сравниваем по языку.
		for _, h := range list {
			if uint64(h)&0xFFFF == v&0xFFFF {
				return h
			}
		}
	}
	for _, h := range list {
		if LangOf(h) == l {
			return h
		}
	}
	return 0
}

// ConsoleLayout читает раскладку консоли через её API: подключается к консоли процесса pid.
// GetKeyboardLayout для окон консоли часто возвращает не то, отсюда этот путь.
func ConsoleLayout(pid uint32) (keymap.Lang, bool) {
	r, _, _ := pAttachConsole.Call(uintptr(pid))
	if r == 0 {
		return keymap.Unknown, false
	}
	defer pFreeConsole.Call()
	var buf [16]uint16 // KL_NAMELENGTH = 9
	r, _, _ = pGetConsoleKeyboardLayoutNameW.Call(uintptr(unsafe.Pointer(&buf[0])))
	if r == 0 {
		return keymap.Unknown, false
	}
	name := syscall.UTF16ToString(buf[:])
	var v uint32
	for _, c := range strings.ToLower(name) {
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | uint32(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | uint32(c-'a'+10)
		}
	}
	l := LangOf(HKL(v))
	return l, l != keymap.Unknown
}

// RequestLayout просит окно переключить раскладку.
func RequestLayout(w WindowInfo, h HKL) {
	if h == 0 {
		return
	}
	target := w.Focus
	if target == 0 {
		target = w.Hwnd
	}
	PostMessage(target, WM_INPUTLANGCHANGEREQUEST, 0, uintptr(h))
	if target != w.Hwnd && w.Hwnd != 0 {
		PostMessage(w.Hwnd, WM_INPUTLANGCHANGEREQUEST, 0, uintptr(h))
	}
}
