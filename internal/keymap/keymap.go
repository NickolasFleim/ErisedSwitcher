// Package keymap описывает, какой символ даёт каждая клавиша в раскладках
// US English, Русская (ЙЦУКЕН) и Украинская (расширенная). Программа хранит не символы,
// а нажатия, поэтому одно и то же нажатие можно «прочитать» в любой раскладке —
// не нужно спрашивать у приложения (терминала), какой текст оно показало.
package keymap

import (
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

// Lang — раскладка.
type Lang int

const (
	Unknown Lang = -1
	EN      Lang = 0
	RU      Lang = 1
	UK      Lang = 2
)

// All — все поддерживаемые раскладки.
var All = []Lang{EN, RU, UK}

func (l Lang) String() string {
	switch l {
	case EN:
		return "en"
	case RU:
		return "ru"
	case UK:
		return "uk"
	}
	return "?"
}

// Cyrillic — кириллическая раскладка.
func (l Lang) Cyrillic() bool { return l == RU || l == UK }

// Включённые языки и «основная» кириллическая раскладка (та, что была последней).
var (
	langMu       sync.RWMutex
	enabled      = []Lang{EN, RU}
	preferredCyr atomic.Int32
)

func init() { preferredCyr.Store(int32(RU)) }

// SetLanguages задаёт, между какими раскладками переключаться (английская есть всегда).
func SetLanguages(ls []Lang) {
	out := []Lang{EN}
	for _, l := range ls {
		if l.Cyrillic() && !containsLang(out, l) {
			out = append(out, l)
		}
	}
	if len(out) == 1 {
		out = append(out, RU)
	}
	langMu.Lock()
	enabled = out
	langMu.Unlock()
	if p := PreferredCyrillic(); !containsLang(out, p) {
		preferredCyr.Store(int32(out[1]))
	}
}

// Languages — включённые языки.
func Languages() []Lang {
	langMu.RLock()
	defer langMu.RUnlock()
	return append([]Lang(nil), enabled...)
}

// Enabled — включён ли язык.
func Enabled(l Lang) bool { return containsLang(Languages(), l) }

func containsLang(ls []Lang, l Lang) bool {
	for _, x := range ls {
		if x == l {
			return true
		}
	}
	return false
}

// SetPreferredCyrillic запоминает, какой кириллической раскладкой вы пользуетесь сейчас.
func SetPreferredCyrillic(l Lang) {
	if l.Cyrillic() && Enabled(l) {
		preferredCyr.Store(int32(l))
	}
}

// PreferredCyrillic — кириллическая раскладка, на которую переключать с английской.
func PreferredCyrillic() Lang { return Lang(preferredCyr.Load()) }

// Other — раскладка, на которую переключать вручную: с английской — на основную кириллическую,
// с кириллической — на английскую.
func (l Lang) Other() Lang {
	switch {
	case l == EN:
		return PreferredCyrillic()
	case l.Cyrillic():
		return EN
	}
	return Unknown
}

// Alternatives — в какие раскладки могло «должно было» попасть слово, набранное в cur.
func Alternatives(cur Lang) []Lang {
	var out []Lang
	for _, l := range Languages() {
		if l != cur {
			out = append(out, l)
		}
	}
	return out
}

// ParseLang разбирает "ru"/"en"/"uk".
func ParseLang(s string) Lang {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "en":
		return EN
	case "ru":
		return RU
	case "uk", "ua":
		return UK
	}
	return Unknown
}

// Key — одно нажатие печатной клавиши.
type Key struct {
	VK    uint16 `json:"vk"`
	Shift bool   `json:"s,omitempty"`
	Caps  bool   `json:"c,omitempty"`
}

// Виртуальные коды клавиш Windows, которые нужны ядру.
const (
	VK_BACK       = 0x08
	VK_TAB        = 0x09
	VK_RETURN     = 0x0D
	VK_SHIFT      = 0x10
	VK_CONTROL    = 0x11
	VK_MENU       = 0x12
	VK_PAUSE      = 0x13
	VK_CAPITAL    = 0x14
	VK_ESCAPE     = 0x1B
	VK_SPACE      = 0x20
	VK_PRIOR      = 0x21
	VK_NEXT       = 0x22
	VK_END        = 0x23
	VK_HOME       = 0x24
	VK_LEFT       = 0x25
	VK_UP         = 0x26
	VK_RIGHT      = 0x27
	VK_DOWN       = 0x28
	VK_INSERT     = 0x2D
	VK_DELETE     = 0x2E
	VK_LWIN       = 0x5B
	VK_RWIN       = 0x5C
	VK_APPS       = 0x5D
	VK_NUMPAD0    = 0x60
	VK_DIVIDE     = 0x6F
	VK_F1         = 0x70
	VK_F24        = 0x87
	VK_SCROLL     = 0x91
	VK_LSHIFT     = 0xA0
	VK_RSHIFT     = 0xA1
	VK_LCONTROL   = 0xA2
	VK_RCONTROL   = 0xA3
	VK_LMENU      = 0xA4
	VK_RMENU      = 0xA5
	VK_OEM_1      = 0xBA // ;:  ж
	VK_OEM_PLUS   = 0xBB // =+
	VK_OEM_COMMA  = 0xBC // ,<  б
	VK_OEM_MINUS  = 0xBD // -_
	VK_OEM_PERIOD = 0xBE // .>  ю
	VK_OEM_2      = 0xBF // /?  .,
	VK_OEM_3      = 0xC0 // `~  ё
	VK_OEM_4      = 0xDB // [{  х
	VK_OEM_5      = 0xDC // \|  \/
	VK_OEM_6      = 0xDD // ]}  ъ
	VK_OEM_7      = 0xDE // '"  э
	VK_OEM_102    = 0xE2 // <> на ISO-клавиатурах: \| и \/
)

type pair struct{ normal, shifted rune }

var tables = [3]map[uint16]pair{
	EN: {}, RU: {}, UK: {},
}

// runeKeys — обратные таблицы: символ -> нажатие (для конвертации готового текста).
var runeKeys = [3]map[rune]Key{{}, {}, {}}

func init() {
	en := "qwertyuiopasdfghjklzxcvbnm"
	ru := "йцукенгшщзфывапролдячсмить"
	letters := "QWERTYUIOPASDFGHJKLZXCVBNM"
	for i, c := range letters {
		vk := uint16(c)
		e := []rune(en)[i]
		r := []rune(ru)[i]
		tables[EN][vk] = pair{e, unicode.ToUpper(e)}
		tables[RU][vk] = pair{r, unicode.ToUpper(r)}
		tables[UK][vk] = pair{r, unicode.ToUpper(r)}
	}
	// Украинская раскладка отличается от русской: і вместо ы, ї вместо ъ, є вместо э,
	// апостроф вместо ё, ґ на клавише \ (расширенная украинская раскладка).
	tables[UK]['S'] = pair{'і', 'І'}
	enDigitsShift := ")!@#$%^&*("
	ruDigitsShift := ")!\"№;%:?*("
	for d := 0; d <= 9; d++ {
		vk := uint16('0' + d)
		tables[EN][vk] = pair{rune('0' + d), []rune(enDigitsShift)[d]}
		tables[RU][vk] = pair{rune('0' + d), []rune(ruDigitsShift)[d]}
		tables[UK][vk] = pair{rune('0' + d), []rune(ruDigitsShift)[d]}
	}
	set := func(vk uint16, en, ru pair) { tables[EN][vk] = en; tables[RU][vk] = ru; tables[UK][vk] = ru }
	set(VK_OEM_1, pair{';', ':'}, pair{'ж', 'Ж'})
	set(VK_OEM_PLUS, pair{'=', '+'}, pair{'=', '+'})
	set(VK_OEM_COMMA, pair{',', '<'}, pair{'б', 'Б'})
	set(VK_OEM_MINUS, pair{'-', '_'}, pair{'-', '_'})
	set(VK_OEM_PERIOD, pair{'.', '>'}, pair{'ю', 'Ю'})
	set(VK_OEM_2, pair{'/', '?'}, pair{'.', ','})
	set(VK_OEM_3, pair{'`', '~'}, pair{'ё', 'Ё'})
	set(VK_OEM_4, pair{'[', '{'}, pair{'х', 'Х'})
	set(VK_OEM_5, pair{'\\', '|'}, pair{'\\', '/'})
	set(VK_OEM_6, pair{']', '}'}, pair{'ъ', 'Ъ'})
	set(VK_OEM_7, pair{'\'', '"'}, pair{'э', 'Э'})
	set(VK_OEM_102, pair{'\\', '|'}, pair{'\\', '/'})
	tables[UK][VK_OEM_6] = pair{'ї', 'Ї'}
	tables[UK][VK_OEM_7] = pair{'є', 'Є'}
	tables[UK][VK_OEM_3] = pair{'\'', '₴'}
	tables[UK][VK_OEM_5] = pair{'ґ', 'Ґ'}

	for _, l := range All {
		for vk, p := range tables[l] {
			for _, c := range []struct {
				r     rune
				shift bool
			}{{p.normal, false}, {p.shifted, true}} {
				// Предпочитаем нажатие без Shift и основной ряд (а не дубли вроде OEM_102).
				if old, ok := runeKeys[l][c.r]; ok && (!old.Shift || c.shift) && old.VK != VK_OEM_102 {
					continue
				}
				runeKeys[l][c.r] = Key{VK: vk, Shift: c.shift}
			}
		}
	}
}

// IsPrintable сообщает, даёт ли клавиша символ (в любой из раскладок).
func IsPrintable(vk uint16) bool {
	if vk == VK_SPACE {
		return true
	}
	_, ok := tables[EN][vk]
	return ok
}

// IsDigit — верхний ряд цифр.
func IsDigit(vk uint16) bool { return vk >= '0' && vk <= '9' }

// Render возвращает символ нажатия в раскладке l.
func Render(k Key, l Lang) rune {
	if k.VK == VK_SPACE {
		return ' '
	}
	if l != EN && l != RU && l != UK {
		l = EN
	}
	p, ok := tables[l][k.VK]
	if !ok {
		return 0
	}
	r := p.normal
	if k.Shift {
		r = p.shifted
	}
	if k.Caps && unicode.IsLetter(r) {
		if unicode.IsUpper(r) {
			r = unicode.ToLower(r)
		} else {
			r = unicode.ToUpper(r)
		}
	}
	return r
}

// RenderAll печатает последовательность нажатий в раскладке l.
func RenderAll(keys []Key, l Lang) string {
	out := make([]rune, 0, len(keys))
	for _, k := range keys {
		if r := Render(k, l); r != 0 {
			out = append(out, r)
		}
	}
	return string(out)
}

// IsLetterIn сообщает, даёт ли клавиша букву в раскладке l.
func IsLetterIn(vk uint16, l Lang) bool {
	if l < EN || l > UK {
		return false
	}
	p, ok := tables[l][vk]
	return ok && unicode.IsLetter(p.normal)
}

// KeyForRune ищет нажатие, которое даёт символ r в раскладке l (без CapsLock).
func KeyForRune(r rune, l Lang) (Key, bool) {
	if r == ' ' {
		return Key{VK: VK_SPACE}, true
	}
	if l < EN || l > UK {
		return Key{}, false
	}
	k, ok := runeKeys[l][r]
	return k, ok
}

// DetectTextLang определяет, в какой раскладке, скорее всего, набран текст:
// по количеству кириллических и латинских букв.
func DetectTextLang(s string) Lang {
	cyr, lat, ru, uk := 0, 0, 0, 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
			switch unicode.ToLower(r) {
			case 'ы', 'э', 'ъ', 'ё':
				ru++
			case 'і', 'ї', 'є', 'ґ':
				uk++
			}
		case unicode.Is(unicode.Latin, r):
			lat++
		}
	}
	if cyr == 0 && lat == 0 {
		// Только знаки: решаем по тому, какие из них «чужие» для кириллицы.
		for _, r := range s {
			if k, ok := runeKeys[EN][r]; ok && Render(k, PreferredCyrillic()) != r {
				return EN
			}
		}
		return Unknown
	}
	if cyr <= lat {
		return EN
	}
	switch {
	case uk > ru:
		return UK
	case ru > uk:
		return RU
	}
	return PreferredCyrillic()
}

// ConvertTextTo переводит текст, набранный в раскладке from, в раскладку to
// (те же клавиши). Символы, которые в from не набрать, остаются как есть.
func ConvertTextTo(s string, from, to Lang) string {
	if from < EN || from > UK || to < EN || to > UK || from == to {
		return s
	}
	out := []rune(s)
	for i, r := range out {
		if k, ok := runeKeys[from][r]; ok {
			if c := Render(k, to); c != 0 {
				out[i] = c
			}
		}
	}
	return string(out)
}

// ConvertText переводит текст из раскладки from в «парную» (from.Other()).
func ConvertText(s string, from Lang) string { return ConvertTextTo(s, from, from.Other()) }
