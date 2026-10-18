package engine

import (
	"strings"
	"unicode"

	"erised/internal/config"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/predict"
)

// SuggestionView — то, что показывает полоска подсказок.
type SuggestionView struct {
	Version uint64
	Window  uintptr
	Items   []SuggestionItem
	// Первый вариант будет применён сам при пробеле (уверенное исправление опечатки).
	AutoFirst bool
	// Слово ещё не начато (варианты следующего слова) — Tab их не принимает, если не включено.
	NextWord bool
	Hints    []string // подписи клавиш для каждого варианта
}

// SuggestionItem — вариант в полоске.
type SuggestionItem struct {
	Text string // как будет вставлено (с учётом регистра)
	Kind predict.Kind
}

type acceptKey struct {
	hk    config.Hotkey
	index int  // какой вариант
	first bool // «принять первый» (Tab): для следующего слова — только если включено
}

type suggestKeys struct {
	accept  []acceptKey
	dismiss config.Hotkey
}

func parseSuggestKeys(c *config.Config) suggestKeys {
	var k suggestKeys
	add := func(s string, idx int, first bool) {
		if h, err := config.ParseHotkey(s); err == nil && !h.Empty && h.VK != 0 {
			k.accept = append(k.accept, acceptKey{h, idx, first})
		}
	}
	add(c.Hotkeys.AcceptFirst, 0, true)
	add(c.Hotkeys.Accept1, 0, false)
	add(c.Hotkeys.Accept2, 1, false)
	add(c.Hotkeys.Accept3, 2, false)
	if h, err := config.ParseHotkey(c.Hotkeys.Dismiss); err == nil {
		k.dismiss = h
	}
	return k
}

func hotkeyIs(h config.Hotkey, ev KeyEvent) bool {
	if h.Empty || h.VK == 0 || h.VK != ev.VK {
		return false
	}
	m := ev.Mods
	return h.Ctrl == m.Ctrl() && h.Alt == m.Alt() && h.Shift == m.Shift() && h.Win == m.Win()
}

// SetPredictor подключает подсказки (без него полоска пустая, опечатки не исправляются).
func (e *Engine) SetPredictor(p *predict.Predictor) { e.mu.Lock(); e.pred = p; e.mu.Unlock() }

// Suggestions — текущие варианты (копия).
func (e *Engine) Suggestions() SuggestionView {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.sv
	v.Items = append([]SuggestionItem(nil), e.sv.Items...)
	v.Hints = append([]string(nil), e.sv.Hints...)
	return v
}

// SuggestVersion — номер текущего набора вариантов (меняется при каждом изменении).
func (e *Engine) SuggestVersion() uint64 { e.mu.Lock(); defer e.mu.Unlock(); return e.sv.Version }

func (e *Engine) setSuggestions(items []SuggestionItem, autoFirst, next bool) {
	same := len(items) == len(e.sv.Items) && autoFirst == e.sv.AutoFirst && next == e.sv.NextWord && e.sv.Window == e.window
	if same {
		for i := range items {
			if items[i] != e.sv.Items[i] {
				same = false
				break
			}
		}
	}
	if same {
		return
	}
	e.sv = SuggestionView{Version: e.sv.Version + 1, Window: e.window, Items: items, AutoFirst: autoFirst, NextWord: next}
	e.sv.Hints = e.hintsFor(len(items), next)
}

func (e *Engine) hintsFor(n int, next bool) []string {
	h := make([]string, n)
	for _, k := range e.sk.accept {
		if k.index >= n || h[k.index] != "" {
			continue
		}
		if k.first && next && !e.cfg.Suggest.TabNextWord {
			continue
		}
		h[k.index] = hotkeyLabel(k.hk)
	}
	return h
}

func hotkeyLabel(h config.Hotkey) string {
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
	name := ""
	switch {
	case h.VK >= '0' && h.VK <= '9', h.VK >= 'A' && h.VK <= 'Z':
		name = string(rune(h.VK))
	case h.VK == keymap.VK_TAB:
		name = "Tab"
	case h.VK == keymap.VK_RETURN:
		name = "Enter"
	case h.VK == keymap.VK_RIGHT:
		name = "→"
	case h.VK >= keymap.VK_F1 && h.VK <= keymap.VK_F24:
		name = "F" + itoa(int(h.VK-keymap.VK_F1+1))
	default:
		name = "#" + itoa(int(h.VK))
	}
	return strings.Join(append(p, name), "+")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// clearSuggestions прячет полоску.
func (e *Engine) clearSuggestions() { e.setSuggestions(nil, false, false) }

// prevWord — предыдущее слово фразы (для подсказок и истории), если между ними только пробел
// и предложение не закончилось.
func (e *Engine) prevWord(l keymap.Lang) string {
	n := len(e.hist)
	if n == 0 {
		return ""
	}
	t := e.hist[n-1]
	if t.trail != keymap.VK_SPACE || len(t.keys) == 0 || t.mixed || t.lang != l {
		return ""
	}
	text := keymap.RenderAll(t.keys, t.lang)
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") ||
		strings.HasSuffix(text, ",") || strings.HasSuffix(text, ";") || strings.HasSuffix(text, ":") {
		return ""
	}
	core, ok := lang.Core(text, t.lang)
	if !ok || strings.ContainsAny(core, "-'") {
		return ""
	}
	return core
}

// sentenceStart — следующее слово начнёт предложение (начало строки или после . ! ?).
func (e *Engine) sentenceStart() bool {
	n := len(e.hist)
	if n == 0 {
		return true
	}
	t := e.hist[n-1]
	if len(t.keys) == 0 {
		return false
	}
	text := strings.TrimRightFunc(keymap.RenderAll(t.keys, t.lang), func(r rune) bool { return r == '"' || r == ')' || r == '»' })
	return strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?")
}

func (e *Engine) autoOptions(r *config.Effective, typed string, l keymap.Lang, typedZipf float64) predict.CorrectOptions {
	o := predict.CorrectOptions{
		MinLen:        r.AutocorrectMinLen,
		MaxDistShort:  r.AutocorrectMaxDistShort,
		MaxDistLong:   r.AutocorrectMaxDistLong,
		MinZipf:       r.AutocorrectMinZipf,
		Margin:        r.AutocorrectMargin,
		EndingCaution: r.AutocorrectEndingCaution,
		TypedZipf:     typedZipf,
		MisspellGap:   2.0,
		Lang:          l,
	}
	if o.EndingCaution {
		if m := lang.Get(l); m != nil {
			o.TypedNaturalness = m.CharScore(typed, true)
		}
	}
	return o
}

// splitWord делит набранное на знаки спереди, слово и знаки сзади: «(првиет,» -> «(», «првиет», «,».
func splitWord(s string) (lead, core, trail string) {
	rs := []rune(s)
	i, j := 0, len(rs)
	for i < j && !unicode.IsLetter(rs[i]) {
		i++
	}
	for j > i && !unicode.IsLetter(rs[j-1]) {
		j--
	}
	return string(rs[:i]), string(rs[i:j]), string(rs[j:])
}

// canAutocorrect — можно ли трогать это слово (регистр, цифры, исключения).
// typedZipf > 0 — слово есть в словаре, но может быть частой ошибкой написания.
func (e *Engine) canAutocorrect(t *token, r *config.Effective) (typedCore string, typedZipf float64, ok bool) {
	if e.pred == nil || r.Autocorrect == "off" || t.mixed || t.corrected || t.seeded || t.lang == keymap.Unknown || len(t.keys) == 0 {
		return "", 0, false
	}
	for _, k := range t.keys {
		if keymap.IsDigit(k.VK) && !k.Shift {
			return "", 0, false
		}
	}
	text := keymap.RenderAll(t.keys, t.lang)
	if r.SkipChars != "" && innerHas(text, r.SkipChars) {
		return "", 0, false
	}
	_, core, _ := splitWord(text)
	low, ok := lang.Core(core, t.lang)
	if !ok || low != strings.ToLower(core) {
		return "", 0, false
	}
	rs := []rune(core)
	upper := 0
	for _, c := range rs {
		if unicode.IsUpper(c) {
			upper++
		}
	}
	switch {
	case upper > 1: // АББРЕВИАТУРА или СловоСЗаглавными
		return "", 0, false
	case upper == 1 && !unicode.IsUpper(rs[0]):
		return "", 0, false
	case upper == 1 && !r.AutocorrectCapitalized && !e.sentenceStart():
		return "", 0, false // скорее всего, имя
	}
	if e.store != nil && (e.store.IsException(t.lang, low) || e.store.IsException(t.lang, strings.ToLower(text)) || e.store.IsWord(t.lang, low)) {
		return "", 0, false
	}
	if e.pred.Exists(t.lang, low) {
		// Существующее слово трогаем, только если это редкая форма из словаря (частая ошибка написания).
		m := lang.Get(t.lang)
		if z, in := m.Zipf(low); in && r.AutocorrectMisspellings && float64(z) <= predict.MisspellMaxZipf {
			return low, float64(z), true
		}
		return "", 0, false
	}
	return low, 0, true
}

// updateSuggestions пересчитывает варианты после нажатия.
func (e *Engine) updateSuggestions(ctx *Context) {
	r := &ctx.Rule
	if e.pred == nil || !r.Predict || e.cfg.Suggest.Position == "off" || !e.enabled || r.Mode == "off" ||
		(ctx.Password && e.cfg.SkipPasswords) {
		e.clearSuggestions()
		return
	}
	n := e.cfg.Suggest.Count
	if len(e.cur.keys) == 0 {
		// Следующее слово.
		if !r.PredictNextWord || len(e.hist) == 0 {
			e.clearSuggestions()
			return
		}
		last := e.hist[len(e.hist)-1]
		prev := e.prevWord(last.lang)
		if prev == "" {
			e.clearSuggestions()
			return
		}
		var items []SuggestionItem
		for _, s := range e.pred.NextWords(last.lang, prev, n, uint32(max(1, e.cfg.Suggest.NextMinCount))) {
			items = append(items, SuggestionItem{Text: s.Word, Kind: predict.Next})
		}
		e.setSuggestions(items, false, true)
		return
	}
	t := &e.cur
	if t.mixed || t.lang == keymap.Unknown {
		e.clearSuggestions()
		return
	}
	text := keymap.RenderAll(t.keys, t.lang)
	lead, core, trail := splitWord(text)
	if trail != "" || core == "" {
		e.clearSuggestions() // после знака препинания слово закончено
		return
	}
	low, ok := lang.Core(core, t.lang)
	if !ok || len([]rune(low)) < r.PredictMinPrefix {
		e.clearSuggestions()
		return
	}
	_ = lead
	prev := e.prevWord(t.lang)
	var items []SuggestionItem
	seen := map[string]bool{}
	add := func(w string, k predict.Kind) {
		if !seen[w] && len(items) < n {
			seen[w] = true
			items = append(items, SuggestionItem{Text: predict.MatchCase(core, w), Kind: k})
		}
	}
	autoFirst := false
	// Опечатка? Тогда исправление — первым. Если же с этих букв начинаются обычные слова,
	// это скорее недописанное слово — исправления не предлагаем.
	if _, tz, can := e.canAutocorrect(t, r); can && r.Autocorrect != "off" && len([]rune(low)) >= 3 &&
		(tz > 0 || !lang.Get(t.lang).HasPrefix(low, 2.0)) {
		cands := e.pred.Correct(t.lang, low, prev, n)
		d := predict.DecideAuto(low, cands, e.autoOptions(r, low, t.lang, tz))
		if tz == 0 || d.OK {
			autoFirst = r.Autocorrect == "auto" && d.OK
			for i, c := range cands {
				if i == 0 || c.Dist <= 1.0 {
					add(c.Word, predict.Correction)
				}
			}
		}
	}
	for _, c := range e.pred.Complete(t.lang, low, prev, n) {
		add(c.Word, predict.Completion)
	}
	if len(items) == 0 {
		e.clearSuggestions()
		return
	}
	if items[0].Kind != predict.Correction {
		autoFirst = false
	}
	e.setSuggestions(items, autoFirst, false)
}

// acceptByKey — нажата клавиша принятия подсказки, пока видна полоска.
func (e *Engine) acceptByKey(ev KeyEvent, ctx *Context) (Result, bool) {
	if !ev.Down || len(e.sv.Items) == 0 || ctx.Window != e.window || e.sv.Window != e.window {
		return Result{}, false
	}
	if hotkeyIs(e.sk.dismiss, ev) {
		e.clearSuggestions()
		return Result{}, true // клавишу пропускаем дальше
	}
	idx := -1
	for _, k := range e.sk.accept {
		if hotkeyIs(k.hk, ev) {
			if k.first && e.sv.NextWord && !e.cfg.Suggest.TabNextWord {
				continue
			}
			idx = k.index
			break
		}
	}
	if idx < 0 && e.cfg.Suggest.AcceptRight && ev.VK == keymap.VK_RIGHT && !ev.Mods.Shift() && !ev.Mods.Ctrl() &&
		!ev.Mods.Alt() && len(e.cur.keys) > 0 {
		idx = 0
	}
	if idx < 0 || idx >= len(e.sv.Items) {
		return Result{}, false
	}
	res := e.accept(idx, ctx)
	res.Swallow = true
	e.swallowUp[ev.VK] = true
	return res, true
}

// AcceptSuggestion — принять вариант щелчком мыши (version — какой набор видел пользователь).
func (e *Engine) AcceptSuggestion(idx int, version uint64, ctx *Context) Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	if version != e.sv.Version || idx < 0 || idx >= len(e.sv.Items) || ctx.Window != e.window {
		return Result{}
	}
	return e.accept(idx, ctx)
}

func (e *Engine) accept(idx int, ctx *Context) Result {
	it := e.sv.Items[idx]
	word := it.Text
	var l keymap.Lang
	backspaces := 0
	prevTok := e.hist
	if len(e.cur.keys) > 0 {
		l = e.cur.lang
		backspaces = len(e.cur.keys)
		text := keymap.RenderAll(e.cur.keys, l)
		lead, _, _ := splitWord(text)
		word = lead + word
	} else {
		if len(e.hist) == 0 {
			return Result{}
		}
		l = e.hist[len(e.hist)-1].lang
		if e.sentenceStart() {
			word = predict.Capitalize(word)
		}
	}
	act := Action{Kind: ActReplace, Backspaces: backspaces, Text: word, Target: keymap.Unknown, Reason: "подсказка"}
	if e.cfg.Suggest.AddSpace {
		act.Trail = []uint16{keymap.VK_SPACE}
	}
	// Буфер: принятое слово становится набранным.
	tok := token{lang: l, corrected: true}
	for _, r := range word {
		k, ok := keymap.KeyForRune(r, l)
		if !ok {
			tok.mixed = true
			break
		}
		tok.keys = append(tok.keys, k)
	}
	_ = prevTok
	e.learnWord(&tok, ctx)
	e.last, e.fresh = nil, false
	if e.cfg.Suggest.AddSpace {
		tok.trail = keymap.VK_SPACE
		e.cur = token{lang: keymap.Unknown}
		e.pushHist(tok)
	} else {
		e.cur = tok
	}
	e.log("подсказка", ctx, Verdict{CurLang: l, Cur: "", Alt: word, Reason: "принят вариант"})
	e.updateSuggestions(ctx)
	return Result{Actions: []Action{act}}
}

// autocorrect — исправление опечатки в законченном слове t (вызывается на пробеле/Enter).
// Возвращает действие и обновлённый токен, если исправили.
func (e *Engine) autocorrect(t *token, vk uint16, ctx *Context) (Action, *correction, bool) {
	r := &ctx.Rule
	if r.Autocorrect != "auto" {
		return Action{}, nil, false
	}
	low, tz, ok := e.canAutocorrect(t, r)
	if !ok {
		return Action{}, nil, false
	}
	prev := e.prevWord(t.lang)
	cands := e.pred.Correct(t.lang, low, prev, 3)
	d := predict.DecideAuto(low, cands, e.autoOptions(r, low, t.lang, tz))
	e.log("опечатка", ctx, Verdict{CurLang: t.lang, Cur: low, Alt: d.Best.Word, Switch: d.OK, Reason: d.Reason,
		AltZipf: float32(d.Best.Zipf), CurScore: d.Best.Dist})
	if !d.OK {
		return Action{}, nil, false
	}
	text := keymap.RenderAll(t.keys, t.lang)
	lead, core, trail := splitWord(text)
	fixed := lead + predict.MatchCase(core, d.Best.Word) + trail
	c := &correction{origKeys: t.keys, origLang: t.lang, newLang: t.lang, newLen: len([]rune(fixed)), trail: vk, auto: true, histIdx: len(e.hist)}
	act := Action{Kind: ActReplace, Backspaces: len(t.keys), Text: fixed, Target: keymap.Unknown, Trail: []uint16{vk}, Reason: "исправлена опечатка", From: text}
	var keys []keymap.Key
	for _, rr := range fixed {
		k, ok := keymap.KeyForRune(rr, t.lang)
		if !ok {
			t.mixed = true
			break
		}
		keys = append(keys, k)
	}
	if !t.mixed {
		t.keys = keys
	}
	t.corrected = true
	return act, c, true
}

// learnWord записывает слово в историю набора (только известные слова).
func (e *Engine) learnWord(t *token, ctx *Context) {
	if e.pred == nil || e.pred.Hist == nil || !ctx.Rule.LearnTyping || t.mixed || len(t.keys) == 0 ||
		(ctx.Password && e.cfg.SkipPasswords) {
		return
	}
	text := keymap.RenderAll(t.keys, t.lang)
	_, core, _ := splitWord(text)
	low, ok := lang.Core(core, t.lang)
	if !ok || strings.ContainsAny(low, "-'") || !e.pred.Known(t.lang, low) {
		return
	}
	e.pred.Hist.Add(t.lang, e.prevWord(t.lang), low)
}
