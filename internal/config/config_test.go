package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Настройки из первой версии (Smena) загружаются, и в них появляются новые параметры.
func TestLoadOldConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	old := `{"version":1,"enabled":true,"double_tap_ms":400,
	 "hotkeys":{"convert_word":"DoubleShift","convert_selection":"DoubleShift"},
	 "apps":[{"name":"Терминалы","enabled":true,"process":"windowsterminal.exe;cmd.exe"}]}`
	if err := os.WriteFile(p, []byte("\xEF\xBB\xBF"+old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("старые настройки не загрузились: %v", err)
	}
	if c.UIMode != "window" {
		t.Fatalf("ui_mode = %q", c.UIMode)
	}
	r := c.Resolve("WindowsTerminal.exe", "", "")
	if r.ReselectAfterConvert {
		t.Fatal("в терминале результат не должен выделяться")
	}
	if !c.Resolve("notepad.exe", "", "").ReselectAfterConvert {
		t.Fatal("в обычных программах результат выделяется")
	}
}

func TestDefaultsValid(t *testing.T) {
	if errs := Default().Validate(); len(errs) > 0 {
		t.Fatal(errs)
	}
}
