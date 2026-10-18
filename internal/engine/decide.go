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
	Switch  bool        `json:"switch"`
	Reason  string      `json:"reason"`
	CurLang keymap.Lang `json:"-"`
	AltLang keymap.Lang `json:"-"`
	AltName string      `json:"alt_lang"` // в какую раскладку переключить
	Cur     string      `json:"cur"`      // как слово выглядит сейчас
	Alt     string      `json:"alt"`      // как оно выглядело бы в другой раскладке
	// Чем заменить, если не просто Alt: знаки по краям слова оставлены как набраны
	// («ghbdtn,» → «привет,», а не «приветб»). Пусто — Alt.
	Text     string  `json:"text,omitempty"`
	CurScore float64 `json:"cur_score"` // оценка букв текущего варианта
	AltScore float64 `json:"alt_score"`
	CurZipf  float32 `json:"cur_zipf"` // частотность (0 — нет в словаре)
	AltZipf  float32 `json:"alt_zipf"`
}

const badScore = -5.0

// Decide решает, набрано ли слово (последовательность нажатий) не в той раскладке, и в какой
// из включённых раскладок оно должно было быть. final=false — проверка на лету (строже).
// prevLang — язык предыдущего слова фразы (контекст; Unknown — нет).
func Decide(keys []keymap.Key, cur keymap.Lang, r *config.Effective, st *words.Store, final bool, prevLang keymap.Lang) Verdict {
	var best *Verdict
	var first Verdict
	for i, alt := range keymap.Alternatives(cur) {
		v := decidePair(keys, cur, alt, r, st, final, prevLang == alt, prevLang == keymap.Unknown)
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
// Если слово целиком не подходит, пробует его без знаков по краям: запятая, точка, кавычка,
// скобка в текущей раскладке — это буквы б, ю, э, х во второй, и «ghbdtn,» иначе стало бы «приветб».
func decidePair(keys []keymap.Key, cur, alt keymap.Lang, r *config.Effective, st *words.Store, final, prevAlt, start bool) Verdict {
	v := decideWhole(keys, cur, alt, r, st, final, prevAlt, start)
	// Целиком — словарное слово: так и оставляем. Целиком подошло только по статистике букв —
	// проверим, не словарное ли слово без знаков по краям («примерб» хуже, чем «пример,»).
	if !final || (v.Switch && v.AltZipf > 0) {
		return v
	}
	lead, trail := punctEdges(keys, cur, alt)
	if lead+trail == 0 || lead+trail >= len(keys) {
		return v
	}
	core := keys[lead : len(keys)-trail]
	v2 := decideWhole(core, cur, alt, r, st, final, prevAlt, start)
	if !v2.Switch || (v.Switch && v2.AltZipf == 0) {
		return v
	}
	v2.Text = keymap.RenderAll(keys[:lead], cur) + v2.Alt + keymap.RenderAll(keys[len(keys)-trail:], cur)
	v2.Cur, v2.Alt = v.Cur, v2.Text
	v2.Reason += "; знаки по краям оставлены как набраны"
	return v2
}

// punctEdges — сколько клавиш в начале и в конце слова в текущей раскладке дают знаки,
// а во второй — буквы.
func punctEdges(keys []keymap.Key, cur, alt keymap.Lang) (lead, trail int) {
	conflict := func(k keymap.Key) bool {
		c, a := keymap.Render(k, cur), keymap.Render(k, alt)
		return c != 0 && !unicode.IsLetter(c) && !unicode.IsDigit(c) && unicode.IsLetter(a)
	}
	for lead < len(keys) && conflict(keys[lead]) {
		lead++
	}
	for trail < len(keys)-lead && conflict(keys[len(keys)-1-trail]) {
		trail++
	}
	return lead, trail
}

func decideWhole(keys []keymap.Key, cur, alt keymap.Lang, r *config.Effective, st *words.Store, final, prevAlt, start bool) Verdict {
	v := Verdict{CurLang: cur, Cur: keymap.RenderAll(keys, cur), Alt: keymap.RenderAll(keys, alt)}
	if cur == keymap.Unknown {
		v.Reason = "раскладка не определена"
		return v
	}
	if r.SkipWithDigits {
		for _, k := range keys {
			if keymap.IsDigit(k.VK) && !k.Shift {
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
	single := n == 1 && final
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
	if single {
		return decideSingle(v, curCore, curOK, altCore, cur, alt, r, st, prevAlt, start)
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

	// «<sk» → «Был», «:tye» → «Жену»: по краям знаки, которые во втором языке — буквы слова.
	// Такое «слово» с отрезанными знаками («sk») может быть в словаре, но слово целиком — нет.
	if lead, trail := punctEdges(keys, cur, alt); lead+trail > 0 && v.AltZipf >= 3.5 && v.AltZipf-v.CurZipf >= 1.0 &&
		!(st != nil && curOK && st.IsWord(cur, curCore)) {
		v.Switch, v.Reason = true, "знаки по краям во втором языке — буквы, и вместе это слово"
		return v
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

// singleWords — слова из одной буквы.
var singleWords = [...]string{
	keymap.EN: "ai",
	keymap.RU: "авикосуя",
	keymap.UK: "авзійоуя",
}

func isSingleWord(l keymap.Lang, core string) bool {
	if int(l) < 0 || int(l) >= len(singleWords) || len([]rune(core)) != 1 {
		return false
	}
	return strings.Contains(singleWords[l], core)
}

// decideSingle — слово из одной буквы: z → я, d → в, ш → I.
// Одна буква — это мало, поэтому исправляем, только если в текущей раскладке такого слова нет,
// а во второй есть, и при этом фраза только началась или предыдущее слово — на втором языке.
// Если предыдущее слово на текущем языке («plan b», «vitamin c»), не трогаем.
func decideSingle(v Verdict, curCore string, curOK bool, altCore string, cur, alt keymap.Lang, r *config.Effective, st *words.Store, prevAlt, start bool) Verdict {
	switch {
	case curOK && isSingleWord(cur, curCore):
		v.Reason = "текущий вариант — слово из одной буквы"
	case !isSingleWord(alt, altCore):
		v.Reason = "одна буква: во втором языке такого слова нет"
	case curOK && st != nil && st.IsWord(cur, curCore):
		v.Reason = "текущий вариант — ваше слово"
	case prevAlt && r.SingleLetterContext:
		v.Switch, v.Reason = true, "слово из одной буквы, а предыдущее слово на втором языке"
	case start && r.SingleLetterStart:
		v.Switch, v.Reason = true, "слово из одной буквы в начале фразы"
	case start:
		v.Reason = "слово из одной буквы в начале фразы (в этой программе не исправляется)"
	default:
		v.Reason = "слово из одной буквы, а предыдущее слово на этом же языке"
	}
	if v.Switch {
		if z, in := lang.Get(alt).Zipf(altCore); in {
			v.AltZipf = z
		}
	}
	return v
}
