//go:build windows

package app

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"erised/internal/win"
)

// Первая версия программы называлась Smena. При первом запуске Erised Switcher:
//   - закрываем работающий Smena, чтобы две программы не исправляли текст одновременно;
//   - убираем его из автозапуска;
//   - переносим его настройки и собранные слова, если своих ещё нет.
func migrateFromSmena(dir string) (notes []string) {
	if h := win.FindWindow("SmenaTrayWindow"); h != 0 {
		win.PostMessage(h, win.WM_CLOSE, 0, 0)
		for i := 0; i < 30 && win.FindWindow("SmenaTrayWindow") != 0; i++ {
			time.Sleep(100 * time.Millisecond)
		}
		notes = append(notes, "старая версия (Smena) закрыта")
	}
	if win.AutostartEnabled("Smena") {
		if err := win.SetAutostart("Smena", "", false); err == nil {
			notes = append(notes, "Smena убрана из автозапуска")
		}
	}
	if os.Getenv("ERISED_HOME") != "" {
		return notes
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return notes
	}
	old := filepath.Join(cfgDir, "Smena")
	if filepath.Clean(old) == filepath.Clean(dir) {
		return notes
	}
	moved := false
	for _, name := range []string{"config.json", "words.json"} {
		src, dst := filepath.Join(old, name), filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			log.Printf("перенос %s: %v", name, err)
			continue
		}
		moved = true
	}
	if moved {
		notes = append(notes, "настройки и слова перенесены из Smena")
	}
	return notes
}
