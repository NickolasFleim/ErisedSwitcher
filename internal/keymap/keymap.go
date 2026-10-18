// Package keymap описывает, какой символ даёт каждая клавиша в раскладках
// US English и Русская (ЙЦУКЕН). Программа хранит не символы, а нажатия,
// поэтому одно и то же нажатие можно «прочитать» в любой раскладке —
// не нужно спрашивать у приложения (терминала), какой текст оно показало.
package keymap

import "unicode"

// Lang — раскладка.
type Lang int

const (
	Unknown Lang = -1
	EN      Lang = 0
	RU      Lang = 1
)

func (l Lang) String() string {
	switch l {
	case EN:
		return "en"
	case RU:
		return "ru"
	}
	return "?"
}

// Other возвращает вторую раскладку пары.
func (l Lang) Other() Lang {
	switch l {
	case EN:
		return RU
	case RU:
		return EN
	}
	return Unknown
}

// ParseLang разбирает "ru"/"en".
func ParseLang(s string) Lang {
	switch s {
	case "en", "EN":
		return EN
	case "ru", "RU":
		return RU
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

var tables = [2]map[uint16]pair{
	EN: {}, RU: {},
}

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
	}
	enDigitsShift := ")!@#$%^&*("
	ruDigitsShift := ")!\"№;%:?*("
	for d := 0; d <= 9; d++ {
		vk := uint16('0' + d)
		tables[EN][vk] = pair{rune('0' + d), []rune(enDigitsShift)[d]}
		tables[RU][vk] = pair{rune('0' + d), []rune(ruDigitsShift)[d]}
	}
	set := func(vk uint16, en, ru pair) { tables[EN][vk] = en; tables[RU][vk] = ru }
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

	for vk, p := range tables[EN] {
		q := tables[RU][vk]
		addConv(p.normal, q.normal)
		addConv(p.shifted, q.shifted)
	}
	// Буквы, набранные с CapsLock+Shift, дают строчные — их тоже нужно уметь переводить.
	for vk := uint16('A'); vk <= 'Z'; vk++ {
		p, q := tables[EN][vk], tables[RU][vk]
		addConv(p.shifted, q.shifted)
	}
}

// Таблицы посимвольной конвертации текста (для выделенного текста).
var (
	enToRu = map[rune]rune{}
	ruToEn = map[rune]rune{}
)

func addConv(e, r rune) {
	if e == r {
		return
	}
	if _, ok := enToRu[e]; !ok {
		enToRu[e] = r
	}
	if _, ok := ruToEn[r]; !ok {
		ruToEn[r] = e
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
	if l != EN && l != RU {
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
	p, ok := tables[l][vk]
	return ok && unicode.IsLetter(p.normal)
}

// KeyForRune ищет нажатие, которое даёт символ r в раскладке l (без CapsLock).
func KeyForRune(r rune, l Lang) (Key, bool) {
	if r == ' ' {
		return Key{VK: VK_SPACE}, true
	}
	for vk, p := range tables[l] {
		if p.normal == r {
			return Key{VK: vk}, true
		}
		if p.shifted == r {
			return Key{VK: vk, Shift: true}, true
		}
	}
	return Key{}, false
}

// DetectTextLang определяет, в какой раскладке, скорее всего, набран текст:
// по количеству кириллических и латинских букв.
func DetectTextLang(s string) Lang {
	ru, en := 0, 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			ru++
		case unicode.Is(unicode.Latin, r):
			en++
		}
	}
	if ru == 0 && en == 0 {
		// Только знаки: решаем по тому, какие из них «чужие».
		for _, r := range s {
			if _, ok := enToRu[r]; ok {
				en++
			}
		}
		if en > 0 {
			return EN
		}
		return Unknown
	}
	if ru > en {
		return RU
	}
	return EN
}

// ConvertText переводит текст, набранный в раскладке from, в другую раскладку.
// Символы, одинаковые в обеих раскладках, остаются как есть.
func ConvertText(s string, from Lang) string {
	m := enToRu
	if from == RU {
		m = ruToEn
	}
	out := []rune(s)
	for i, r := range out {
		if c, ok := m[r]; ok {
			out[i] = c
		}
	}
	return string(out)
}
