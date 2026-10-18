package engine

import (
	"strings"
	"unicode"

	"erised/internal/config"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/words"
)

// Verdict — результат проверки слова с объяснением (виден в журнале и на вкладке «Проверка»).
type Verdict struct {
	Switch   bool        `json:"switch"`
	Reason   string      `json:"reason"`
	CurLang  keymap.Lang `json:"-"`
	AltLang  keymap.Lang `json:"-"`
	AltName  string      `json:"alt_lang"`  // в какую раскладку переключить
	Cur      string      `json:"cur"`       // как слово выглядит сейчас
	Alt      string      `json:"alt"`       // как оно выглядело бы в другой раскладке
	CurScore float64     `json:"cur_score"` // оценка букв текущего варианта
	AltScore float64     `json:"alt_score"`
	CurZipf  float32     `json:"cur_zipf"` // частотность (0 — нет в словаре)
	AltZipf  float32     `json:"alt_zipf"`
}

const badScore = -5.0

// Decide решает, набрано ли слово (последовательность нажатий) не в той раскладке, и в какой
// из включённых раскладок оно должно было быть. final=false — проверка на лету (строже).
// prevLang — язык предыдущего слова фразы (контекст; Unknown — нет).
func Decide(keys []keymap.Key, cur keymap.Lang, r *config.Effective, st *words.Store, final bool, prevLang keymap.Lang) Verdict {
	var best *Verdict
	var first Verdict
	for i, alt := range keymap.Alternatives(cur) {
		v := decidePair(keys, cur, alt, r, st, final, prevLang == alt)
		v.AltLang, v.AltName = alt, alt.String()
		if i == 0 || alt == cur.Other() {
			first = v
		}
		if v.Switch && (best == nil || betterSwitch(v, *best)) {
			vv := v
			best = &vv
		}
	}
	if best != nil {
		return *best
	}
	if first.CurLang == 0 && first.Cur == "" { // нет альтернатив
		first = Verdict{CurLang: cur, Cur: keymap.RenderAll(keys, cur), Reason: "нет других раскладок"}
	}
	return first
}

// betterSwitch — из двух подходящих раскладок выбрать лучшую: словарное слово лучше
// статистики; между русским и украинским — та, которой вы пользуетесь, если другая
// не намного частотнее; иначе — более частое слово.
func betterSwitch(a, b Verdict) bool {
	ad, bd := a.AltZipf > 0, b.AltZipf > 0
	if ad != bd {
		return ad
	}
	if a.AltLang.Cyrillic() && b.AltLang.Cyrillic() && ad {
		pref := keymap.PreferredCyrillic()
		switch {
		case a.AltLang == pref && b.AltZipf < a.AltZipf+1.5:
			return true
		case b.AltLang == pref && a.AltZipf < b.AltZipf+1.5:
			return false
		}
	}
	if a.AltZipf != b.AltZipf {
		return a.AltZipf > b.AltZipf
	}
	return a.AltScore > b.AltScore
}

// decidePair — набрано ли слово в cur, а должно было быть в alt.
func decidePair(keys []keymap.Key, cur, alt keymap.Lang, r *config.Effective, st *words.Store, final, prevAlt bool) Verdict {
	v := Verdict{CurLang: cur, Cur: keymap.RenderAll(keys, cur), Alt: keymap.RenderAll(keys, alt)}
	if cur == keymap.Unknown {
		v.Reason = "раскладка не определена"
		return v
	}
	if r.SkipWithDigits {
		for _, k := range keys {
			if keymap.IsDigit(k.VK) {
				v.Reason = "в слове есть цифры"
				return v
			}
		}
	}
	if r.SkipChars != "" && innerHas(v.Cur, r.SkipChars) {
		v.Reason = "внутри слова есть символ из списка «не исправлять»"
		return v
	}
	altCore, ok := lang.Core(v.Alt, alt)
	if !ok {
		v.Reason = "во второй раскладке это не слово"
		return v
	}
	if !edgesOK(keys, v.Alt, cur) {
		v.Reason = "во второй раскладке буквы по краям превращаются в знаки"
		return v
	}
	curCore, curOK := lang.Core(v.Cur, cur)
	if curOK && r.Prefer[curCore] {
		v.Reason = "слово из списка приложения"
		return v
	}
	if r.Prefer[altCore] && final {
		v.Switch, v.Reason = true, "второй вариант — слово из списка приложения"
		return v
	}
	n := len([]rune(altCore))
	single := n == 1 && final && prevAlt && r.SingleLetterContext
	if n < r.MinLen && !single {
		v.Reason = "слишком короткое"
		return v
	}
	if st != nil {
		if st.IsException(cur, strings.ToLower(v.Cur)) || (curOK && st.IsException(cur, curCore)) {
			v.Reason = "слово в исключениях"
			return v
		}
	}
	if cur.Cyrillic() && alt.Cyrillic() {
		return decideCyrillic(v, curCore, curOK, altCore, cur, alt, st, final)
	}
	cm, am := lang.Get(cur), lang.Get(alt)
	v.CurScore = badScore
	if curOK {
		v.CurScore = cm.CharScore(curCore, final)
		if z, in := cm.Zipf(curCore); in {
			v.CurZipf = z
		}
	}
	v.AltScore = am.CharScore(altCore, final)
	if z, in := am.Zipf(altCore); in {
		v.AltZipf = z
	}

	if !final {
		return decideEarly(v, curCore, curOK, altCore, cm, am, r, st, cur, alt)
	}

	if curOK && (v.CurZipf > 0 || (st != nil && st.IsWord(cur, curCore))) {
		v.Reason = "текущий вариант — известное слово"
		return v
	}
	if st != nil && st.IsWord(alt, altCore) {
		v.Switch, v.Reason = true, "второй вариант — ваше слово"
		return v
	}
	if v.AltZipf > 0 {
		if n == 1 {
			if single && v.AltZipf >= 4.5 {
				v.Switch, v.Reason = true, "однобуквенное слово, а предыдущее слово на втором языке"
			} else {
				v.Reason = "одна буква: недостаточно уверенности"
			}
			return v
		}
		if n <= 3 {
			switch {
			case v.AltZipf < float32(r.ShortWordMinZipf):
				v.Reason = "короткое слово: во втором языке оно редкое"
			case v.CurScore <= r.ShortWordCurMax:
				v.Switch, v.Reason = true, "короткое частое слово во втором языке"
			case v.AltZipf >= 5.0:
				v.Switch, v.Reason = true, "очень частое короткое слово во втором языке"
			case prevAlt:
				v.Switch, v.Reason = true, "короткое слово, а предыдущее слово на втором языке"
			default:
				v.Reason = "короткое слово: недостаточно уверенности"
			}
			return v
		}
		switch {
		case v.CurScore <= r.DictCurMax:
			v.Switch, v.Reason = true, "второй вариант есть в словаре"
		case v.AltZipf >= 4.0:
			v.Switch, v.Reason = true, "второй вариант — частое слово"
		case prevAlt:
			v.Switch, v.Reason = true, "второй вариант в словаре, предыдущее слово на втором языке"
		default:
			v.Reason = "второй вариант в словаре, но текущий тоже похож на слово"
		}
		return v
	}
	if r.DictionaryOnly {
		v.Reason = "второго варианта нет в словаре (режим «только по словарю»)"
		return v
	}
	if n < r.NgramMinLen {
		v.Reason = "незнакомое слово короче порога статистики"
		return v
	}
	if v.CurScore <= r.NgramCurMax && v.AltScore >= r.NgramAltMin && v.AltScore-v.CurScore >= r.NgramMargin {
		v.Switch, v.Reason = true, "по статистике букв второй вариант намного вероятнее"
		return v
	}
	v.Reason = "по статистике букв уверенности недостаточно"
	return v
}

// decideCyrillic — русский ↔ украинский. Раскладки отличаются всего несколькими буквами
// (ы/і, ъ/ї, э/є, ё/'), а языки похожи, поэтому только по словарю и только в конце слова:
// второй вариант должен быть известным словом, а набранный — нет.
func decideCyrillic(v Verdict, curCore string, curOK bool, altCore string, cur, alt keymap.Lang, st *words.Store, final bool) Verdict {
	switch {
	case v.Cur == v.Alt:
		v.Reason = "в обеих раскладках одинаково"
		return v
	case !final:
		v.Reason = "русский и украинский различаем только по целому слову"
		return v
	}
	cm, am := lang.Get(cur), lang.Get(alt)
	if curOK {
		v.CurScore = cm.CharScore(curCore, true)
		if z, in := cm.Zipf(curCore); in {
			v.CurZipf = z
		}
		if cm.Exists(curCore) || (st != nil && st.IsWord(cur, curCore)) {
			v.Reason = "текущий вариант — существующее слово"
			return v
		}
	}
	v.AltScore = am.CharScore(altCore, true)
	if z, in := am.Zipf(altCore); in {
		v.AltZipf = z
	}
	if st != nil && st.IsWord(alt, altCore) {
		v.Switch, v.Reason = true, "второй вариант — ваше слово"
		return v
	}
	if v.AltZipf > 0 && len([]rune(altCore)) >= 2 {
		v.Switch, v.Reason = true, "слово набрано в другой кириллической раскладке"
		return v
	}
	v.Reason = "второго варианта нет в словаре"
	return v
}

func decideEarly(v Verdict, curCore string, curOK bool, altCore string, cm, am *lang.Model, r *config.Effective, st *words.Store, cur, alt keymap.Lang) Verdict {
	// Посреди слова переключаемся, только если текущее начало не может быть началом
	// ни одного слова, а второе — начало частого слова.
	if curOK && cm.HasPrefix(curCore, 0) {
		v.Reason = "текущее начало встречается в словах"
		return v
	}
	if curOK && st != nil && st.IsWord(cur, curCore) {
		v.Reason = "текущий вариант — ваше слово"
		return v
	}
	if !am.HasPrefix(altCore, 2.5) {
		v.Reason = "второй вариант не начало частого слова"
		return v
	}
	if v.CurScore > r.NgramCurMax-0.2 || v.AltScore < r.NgramAltMin || v.AltScore-v.CurScore < r.NgramMargin {
		v.Reason = "на лету: уверенности недостаточно"
		return v
	}
	v.Switch, v.Reason = true, "на лету: начало слова в другой раскладке"
	return v
}

// innerHas — есть ли в s (без краёв, которые не буквы) символ из set.
func innerHas(s, set string) bool {
	rs := []rune(s)
	i, j := 0, len(rs)
	for i < j && !unicode.IsLetter(rs[i]) {
		i++
	}
	for j > i && !unicode.IsLetter(rs[j-1]) {
		j--
	}
	for _, r := range rs[i:j] {
		if strings.ContainsRune(set, r) {
			return true
		}
	}
	return false
}

// edgesOK: знаки, которые во втором варианте отбрасываются по краям, не должны быть
// буквами текущего варианта. Исключения: одна такая буква в конце (запятая или точка
// после слова) и кавычки в начале.
func edgesOK(keys []keymap.Key, alt string, cur keymap.Lang) bool {
	rs := []rune(alt)
	if len(rs) != len(keys) {
		return true
	}
	for i := 0; i < len(rs) && !unicode.IsLetter(rs[i]); i++ {
		if keymap.IsLetterIn(keys[i].VK, cur) && rs[i] != '"' && rs[i] != '\'' {
			return false
		}
	}
	trail := 0
	for j := len(rs) - 1; j >= 0 && !unicode.IsLetter(rs[j]); j-- {
		if keymap.IsLetterIn(keys[j].VK, cur) {
			trail++
		}
	}
	return trail <= 1
}
