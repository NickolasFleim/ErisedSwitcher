//go:build windows

package app

import (
	"log"
	"path/filepath"
	"time"

	"erised/internal/engine"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/predict"
	"erised/internal/win"
)

// initPredict подключает подсказки: история набора, предсказатель, полоска.
func (a *App) initPredict() {
	path := filepath.Join(a.dir, "typing.json")
	h, err := predict.LoadHistory(path)
	if err != nil {
		log.Printf("typing.json: %v (история набора начнётся заново)", err)
		_ = copyFile(path, path+".broken")
		h = predict.NewHistory(path)
	}
	if n := a.cfg.Load().Suggest.MaxPairs; n > 0 {
		h.MaxPairs = n
	}
	a.hist = h
	a.pred = &predict.Predictor{Hist: h, Store: a.store}
	a.eng.SetPredictor(a.pred)
	a.popup = win.NewPopup(a.onPopupClick)
	// Индекс похожих слов строится ~1 с — в фоне, чтобы первое исправление не ждало.
	go prepareSimilar()
}

// pushSuggestions передаёт полоске текущие варианты (вызывается из потока перехвата).
func (a *App) pushSuggestions(info *win.WindowInfo) {
	if a.popup == nil {
		return
	}
	v := a.eng.Suggestions()
	sg := a.cfg.Load().Suggest
	if len(v.Items) == 0 || sg.Position == "off" {
		a.popup.Hide()
		return
	}
	items := make([]win.PopupItem, len(v.Items))
	for i, it := range v.Items {
		items[i] = win.PopupItem{Text: it.Text, Bold: i == 0 && v.AutoFirst}
		if sg.ShowHints && i < len(v.Hints) {
			items[i].Hint = v.Hints[i]
		}
	}
	a.popup.Show(win.PopupState{
		Items: items, Version: v.Version,
		Target: info.Hwnd, Focus: info.Focus, Thread: info.Thread,
		Position: sg.Position, Fallback: sg.Fallback, UseAcc: sg.UseAccessibility,
		Scale: sg.Scale, Clickable: sg.AcceptClick,
	})
	a.popupWindow.Store(uint64(info.Hwnd))
}

// onPopupClick — щелчок по варианту в полоске (поток полоски).
func (a *App) onPopupClick(idx int, version uint64) {
	info := win.Describe(false)
	cfg := a.cfg.Load()
	rule := cfg.Resolve(info.Process, info.Class, info.Title)
	ctx := &engine.Context{Window: info.Hwnd, App: info.Process, Class: info.Class, Layout: keymap.Unknown, Password: info.Password, Rule: rule}
	res := a.eng.AcceptSuggestion(idx, version, ctx)
	if len(res.Actions) > 0 {
		a.busyMu.Lock()
		a.busy, a.busySince = true, time.Now()
		a.busyMu.Unlock()
		a.jobs <- job{actions: res.Actions, info: info, rule: rule}
	}
	a.pushSuggestions(&info)
}

// checkPopup прячет полоску, если вы переключились в другое окно (таймер, главный поток).
func (a *App) checkPopup() {
	if a.popup == nil || !a.popup.Visible() {
		return
	}
	if fg := win.ForegroundWindow(); uint64(fg) != a.popupWindow.Load() {
		a.eng.Reset()
		a.popup.Hide()
	}
}

// ---------- для окна настроек ----------

// HistoryStats — сводка истории набора.
func (a *App) HistoryStats() predict.Stats {
	if a.hist == nil {
		return predict.Stats{}
	}
	return a.hist.Stats(200)
}

// HistoryClear стирает историю набора.
func (a *App) HistoryClear() error {
	if a.hist == nil {
		return nil
	}
	a.hist.Clear()
	return a.hist.SaveIfDirty()
}

// HistoryForget удаляет пару слов из истории.
func (a *App) HistoryForget(l, prev, next string) error {
	if a.hist == nil {
		return nil
	}
	a.hist.Forget(keymap.ParseLang(l), prev, next)
	return a.hist.SaveIfDirty()
}

// Predictor — для вкладки «Проверка».
func (a *App) Predictor() *predict.Predictor { return a.pred }

// prepareSimilar строит индексы похожих слов для включённых языков (в фоне, ~1 с на язык).
func prepareSimilar() {
	for _, l := range keymap.Languages() {
		lang.Get(l).PrepareSimilar()
	}
}
