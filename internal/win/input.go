//go:build windows

package win

import (
	"time"
	"unicode/utf16"
	"unsafe"

	"erised/internal/keymap"
)

// Метки в dwExtraInfo: так хук отличает свои нажатия от настоящих.
const (
	MagicOwn    uintptr = 0x534D454E // «SMEN» — наш ввод, не обрабатывать
	MagicReplay uintptr = 0x534D4552 // «SMER» — повтор ваших нажатий, обработать как настоящие
)

// SendInputs отправляет события в систему.
func SendInputs(in []INPUT) {
	if len(in) == 0 {
		return
	}
	pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(INPUT{}))
}

func isExtended(vk uint16) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E, // навигация
		0x5B, 0x5C, 0x5D, 0xA3, 0xA5, 0x6F, 0x90, 0x2C:
		return true
	}
	return false
}

// VKInput — событие клавиши.
func VKInput(vk uint16, up bool, extra uintptr) INPUT {
	var f uint32
	if up {
		f |= KEYEVENTF_KEYUP
	}
	if isExtended(vk) {
		f |= KEYEVENTF_EXTENDEDKEY
	}
	return INPUT{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{Vk: vk, Flags: f, ExtraInfo: extra}}
}

// RawInput — повтор перехваченного события как есть.
func RawInput(vk, scan uint16, flags uint32, up bool, extra uintptr) INPUT {
	var f uint32
	if up {
		f |= KEYEVENTF_KEYUP
	}
	if flags&LLKHF_EXTENDED != 0 {
		f |= KEYEVENTF_EXTENDEDKEY
	}
	return INPUT{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{Vk: vk, Scan: scan, Flags: f, ExtraInfo: extra}}
}

func tap(vk uint16) []INPUT {
	return []INPUT{VKInput(vk, false, MagicOwn), VKInput(vk, true, MagicOwn)}
}

func unicodeInputs(s string) []INPUT {
	var in []INPUT
	for _, u := range utf16.Encode([]rune(s)) {
		in = append(in,
			INPUT{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{Scan: u, Flags: KEYEVENTF_UNICODE, ExtraInfo: MagicOwn}},
			INPUT{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{Scan: u, Flags: KEYEVENTF_UNICODE | KEYEVENTF_KEYUP, ExtraInfo: MagicOwn}})
	}
	return in
}

// send отправляет события пачкой или по одному нажатию с паузой.
func send(in []INPUT, per int, delay time.Duration) {
	if delay <= 0 {
		SendInputs(in)
		return
	}
	for i := 0; i < len(in); i += per {
		j := i + per
		if j > len(in) {
			j = len(in)
		}
		SendInputs(in[i:j])
		time.Sleep(delay)
	}
}

// Backspaces стирает n символов.
func Backspaces(n int, delay time.Duration) {
	var in []INPUT
	for i := 0; i < n; i++ {
		in = append(in, tap(keymap.VK_BACK)...)
	}
	send(in, 2, delay)
}

// TypeUnicode печатает текст символами Unicode (не зависит от раскладки).
func TypeUnicode(s string, delay time.Duration) {
	send(unicodeInputs(s), 2, delay)
}

// TypeKeys нажимает те же клавиши (символы зависят от текущей раскладки окна).
func TypeKeys(keys []keymap.Key, delay time.Duration) {
	var in []INPUT
	for _, k := range keys {
		if k.Shift {
			in = append(in, VKInput(keymap.VK_LSHIFT, false, MagicOwn))
		}
		in = append(in, tap(k.VK)...)
		if k.Shift {
			in = append(in, VKInput(keymap.VK_LSHIFT, true, MagicOwn))
		}
	}
	send(in, 1, delay/2)
}

// SelectBack выделяет n символов слева от курсора (Shift+Left).
func SelectBack(n int, delay time.Duration) {
	in := []INPUT{VKInput(keymap.VK_LSHIFT, false, MagicOwn)}
	for i := 0; i < n; i++ {
		in = append(in, tap(keymap.VK_LEFT)...)
	}
	in = append(in, VKInput(keymap.VK_LSHIFT, true, MagicOwn))
	send(in, 2, delay)
}

// TapKeys нажимает служебные клавиши (пробел, Enter, Tab).
func TapKeys(vks []uint16, delay time.Duration) {
	var in []INPUT
	for _, vk := range vks {
		in = append(in, tap(vk)...)
	}
	send(in, 2, delay)
}

// Combo нажимает сочетание вроде Ctrl+Shift+C.
func Combo(ctrl, alt, shift, winKey bool, vk uint16) {
	var down, up []INPUT
	for _, m := range []struct {
		on bool
		vk uint16
	}{{ctrl, keymap.VK_LCONTROL}, {alt, keymap.VK_LMENU}, {shift, keymap.VK_LSHIFT}, {winKey, keymap.VK_LWIN}} {
		if m.on {
			down = append(down, VKInput(m.vk, false, MagicOwn))
			up = append([]INPUT{VKInput(m.vk, true, MagicOwn)}, up...)
		}
	}
	in := append(down, tap(vk)...)
	in = append(in, up...)
	SendInputs(in)
}

var modifierVKs = []uint16{keymap.VK_LSHIFT, keymap.VK_RSHIFT, keymap.VK_LCONTROL, keymap.VK_RCONTROL,
	keymap.VK_LMENU, keymap.VK_RMENU, keymap.VK_LWIN, keymap.VK_RWIN}

// WaitModifiersReleased ждёт, пока вы отпустите Shift/Ctrl/Alt/Win (после горячей клавиши),
// и если не дождался — отпускает их программно, чтобы Backspace не превратился в Ctrl+Backspace.
func WaitModifiersReleased(max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		held := false
		for _, vk := range modifierVKs {
			if GetAsyncKeyDown(int(vk)) {
				held = true
				break
			}
		}
		if !held {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Пустое нажатие (0xE8 не назначен) перед отпусканием, чтобы одиночное отпускание
	// Alt/Win не открыло меню окна или «Пуск».
	in := tap(0xE8)
	n := len(in)
	for _, vk := range modifierVKs {
		if GetAsyncKeyDown(int(vk)) {
			in = append(in, VKInput(vk, true, MagicOwn))
		}
	}
	if len(in) > n {
		SendInputs(in)
	}
}
