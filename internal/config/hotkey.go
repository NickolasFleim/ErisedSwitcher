package config

import (
	"fmt"
	"strings"
)

// TapKey — модификатор для одиночного или двойного нажатия «самого по себе».
type TapKey int

const (
	TapNone TapKey = iota
	TapShift
	TapLShift
	TapRShift
	TapCtrl
	TapLCtrl
	TapRCtrl
	TapAlt
	TapLAlt
	TapRAlt
	TapWin
)

var tapNames = map[string]TapKey{
	"shift": TapShift, "lshift": TapLShift, "rshift": TapRShift,
	"ctrl": TapCtrl, "lctrl": TapLCtrl, "rctrl": TapRCtrl,
	"alt": TapAlt, "lalt": TapLAlt, "ralt": TapRAlt, "win": TapWin,
}

// Hotkey — разобранная горячая клавиша.
type Hotkey struct {
	Empty  bool
	Double bool   // двойное нажатие модификатора (DoubleShift)
	Tap    TapKey // модификатор (для Double или одиночного нажатия, например "RCtrl")
	VK     uint16 // основная клавиша комбинации
	Ctrl   bool
	Alt    bool
	Shift  bool
	Win    bool
}

var keyNames = map[string]uint16{
	"pause": 0x13, "break": 0x13, "capslock": 0x14, "caps": 0x14, "scrolllock": 0x91, "scroll": 0x91,
	"numlock": 0x90, "insert": 0x2D, "ins": 0x2D, "delete": 0x2E, "del": 0x2E, "home": 0x24, "end": 0x23,
	"pageup": 0x21, "pgup": 0x21, "pagedown": 0x22, "pgdn": 0x22, "space": 0x20, "tab": 0x09,
	"enter": 0x0D, "return": 0x0D, "esc": 0x1B, "escape": 0x1B, "backspace": 0x08, "apps": 0x5D,
	"menu": 0x5D, "printscreen": 0x2C, "prtsc": 0x2C, "left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
	"`": 0xC0, "~": 0xC0, "grave": 0xC0, "-": 0xBD, "minus": 0xBD, "=": 0xBB, "plus": 0xBB,
	"[": 0xDB, "]": 0xDD, "\\": 0xDC, ";": 0xBA, "'": 0xDE, ",": 0xBC, ".": 0xBE, "/": 0xBF,
}

// ParseHotkey разбирает строку. Пустая строка или "None" — горячая клавиша выключена.
func ParseHotkey(s string) (Hotkey, error) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if low == "" || low == "none" || low == "нет" {
		return Hotkey{Empty: true}, nil
	}
	if strings.HasPrefix(low, "double") {
		t, ok := tapNames[strings.TrimSpace(low[6:])]
		if !ok {
			return Hotkey{}, fmt.Errorf("неизвестный модификатор в %q (можно Shift, LShift, RShift, Ctrl, LCtrl, RCtrl, Alt, LAlt, RAlt, Win)", s)
		}
		return Hotkey{Double: true, Tap: t}, nil
	}
	if t, ok := tapNames[low]; ok {
		if t == TapShift || t == TapCtrl || t == TapAlt {
			return Hotkey{}, fmt.Errorf("одиночное нажатие %q слишком легко задеть; используйте левый/правый вариант (L/R) или Double", s)
		}
		return Hotkey{Tap: t}, nil
	}
	var h Hotkey
	parts := strings.Split(low, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		last := i == len(parts)-1
		if p == "" && last && i > 0 {
			p = "+" // "Ctrl++"
		}
		switch p {
		case "ctrl", "control":
			h.Ctrl = true
			continue
		case "alt":
			h.Alt = true
			continue
		case "shift":
			h.Shift = true
			continue
		case "win":
			h.Win = true
			continue
		}
		if !last {
			return Hotkey{}, fmt.Errorf("в %q модификатор %q не распознан", s, p)
		}
		vk, err := parseKey(p)
		if err != nil {
			return Hotkey{}, fmt.Errorf("%q: %v", s, err)
		}
		h.VK = vk
	}
	if h.VK == 0 {
		return Hotkey{}, fmt.Errorf("в %q нет основной клавиши", s)
	}
	return h, nil
}

func parseKey(p string) (uint16, error) {
	if vk, ok := keyNames[p]; ok {
		return vk, nil
	}
	if len(p) == 1 {
		c := p[0]
		if c >= 'a' && c <= 'z' {
			return uint16(c - 'a' + 'A'), nil
		}
		if c >= '0' && c <= '9' {
			return uint16(c), nil
		}
	}
	if len(p) >= 2 && p[0] == 'f' {
		n := 0
		if _, err := fmt.Sscanf(p[1:], "%d", &n); err == nil && n >= 1 && n <= 24 {
			return uint16(0x70 + n - 1), nil
		}
	}
	if strings.HasPrefix(p, "vk") {
		var n int
		if _, err := fmt.Sscanf(p[2:], "%x", &n); err == nil && n > 0 && n < 256 {
			return uint16(n), nil
		}
	}
	return 0, fmt.Errorf("неизвестная клавиша %q", p)
}

// String — обратно в строку (для отображения).
func (h Hotkey) String() string {
	if h.Empty {
		return ""
	}
	var tn string
	for n, t := range tapNames {
		if t == h.Tap {
			tn = n
		}
	}
	if h.Double {
		return "Double" + tn
	}
	if h.VK == 0 {
		return tn
	}
	var p []string
	if h.Ctrl {
		p = append(p, "Ctrl")
	}
	if h.Alt {
		p = append(p, "Alt")
	}
	if h.Shift {
		p = append(p, "Shift")
	}
	if h.Win {
		p = append(p, "Win")
	}
	p = append(p, fmt.Sprintf("vk%02X", h.VK))
	return strings.Join(p, "+")
}
