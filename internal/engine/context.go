package engine

import (
	"strings"
	"unicode"

	"erised/internal/keymap"
)

// Epoch — номер «чистого листа»: меняется при каждом сбросе буфера (щелчок, смена окна).
// Чтение текста у курсора идёт в фоне; результат применяется, только если с тех пор
// ничего не произошло.
func (e *Engine) Epoch() uint64 { e.mu.Lock(); defer e.mu.Unlock(); return e.epoch }

// SeedContext — текст перед курсором ввода, прочитанный из окна после щелчка мышью
// (специальные возможности Windows). Слова становятся «набранными»: двойной Shift
// конвертирует последнее слово или фразу, подсказки знают предыдущее слово.
// window — окно, из которого прочитан текст. Сами по себе прочитанные слова не исправляются. afterWord — сразу за курсором буква:
// курсор посреди слова, тогда не делаем ничего. Возвращает, применено ли.
func (e *Engine) SeedContext(epoch uint64, window uintptr, before string, afterWord bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if epoch != e.epoch || len(e.cur.keys) > 0 || len(e.hist) > 0 || afterWord {
		return false
	}
	toks, cur := seedTokens(before)
	if len(toks) == 0 && len(cur.keys) == 0 {
		return false
	}
	e.hist = toks
	e.cur = cur
	if e.window != window {
		e.window = window
		e.selection, e.selByMouse = false, false
	}
	return true
}

// seedTokens разбивает текст текущей строки перед курсором на слова, как если бы их
// набрали: слово — до пробела. Два пробела подряд или непредставимый символ начинают
// фразу заново (так же ведёт себя набор).
func seedTokens(before string) (hist []token, cur token) {
	cur = token{lang: keymap.Unknown}
	if i := strings.LastIndexAny(before, "\r\n\t"); i >= 0 {
		before = before[i+1:]
	}
	if r := []rune(before); len(r) > 80 {
		before = string(r[len(r)-80:])
		// Первое слово может быть обрезано — отбрасываем его.
		if i := strings.IndexByte(before, ' '); i >= 0 {
			before = before[i+1:]
		} else {
			return nil, cur
		}
	}
	parts := strings.Split(before, " ")
	for i, p := range parts {
		last := i == len(parts)-1
		if p == "" {
			if !last {
				hist = nil // два пробела подряд или пробел в начале
			}
			continue
		}
		t, ok := wordToken(p)
		if !ok {
			hist, cur = nil, token{lang: keymap.Unknown}
			continue
		}
		if last {
			cur = t
		} else {
			t.trail = keymap.VK_SPACE
			hist = append(hist, t)
		}
	}
	if len(hist) > 8 {
		hist = hist[len(hist)-8:]
	}
	return hist, cur
}

func wordToken(w string) (token, bool) {
	l := keymap.DetectTextLang(w)
	if l == keymap.Unknown {
		l = keymap.EN
	}
	if !keymap.Enabled(l) {
		return token{}, false
	}
	t := token{lang: l, seeded: true}
	letters := false
	for _, r := range w {
		k, ok := keymap.KeyForRune(r, l)
		if !ok {
			return token{}, false
		}
		if unicode.IsLetter(r) {
			letters = true
		}
		t.keys = append(t.keys, k)
	}
	return t, letters && len(t.keys) <= 64
}
