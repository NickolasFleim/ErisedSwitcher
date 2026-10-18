//go:build windows

package app

import (
	"fmt"
	"strings"

	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/words"
)

// Stats — замер точности для окна настроек.
func (a *App) Stats() map[string]any {
	if a.stats == nil {
		return map[string]any{}
	}
	return a.stats.Summary(30)
}

// StatsAction — «верно»/«ошибка» при разборе примера, сброс статистики.
// «Ошибка» добавляет набранное слово в исключения, чтобы программа его больше не трогала.
func (a *App) StatsAction(op string, id int64, verdict string) error {
	if a.stats == nil {
		return fmt.Errorf("статистика недоступна")
	}
	switch op {
	case "clear":
		a.stats.Clear()
	case "mark":
		e, ok := a.stats.Mark(id, verdict)
		if !ok {
			return fmt.Errorf("пример не найден")
		}
		if verdict == "wrong" && strings.HasPrefix(e.Kind, "would_") {
			l := keymap.ParseLang(e.Lang)
			if core, ok := lang.Core(e.From, l); ok {
				cfg := a.cfg.Load()
				a.store.Observe(words.KindException, l, core, e.App, 1, false, cfg.Learn.MaxCandidates)
				return a.SaveWords()
			}
		}
	default:
		return fmt.Errorf("неизвестная операция %q", op)
	}
	return nil
}
