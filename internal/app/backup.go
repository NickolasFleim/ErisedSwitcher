//go:build windows

package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"erised/internal/config"
	"erised/internal/settingswin"
	"erised/internal/win"
	"erised/internal/words"
)

// Экспорт и импорт всех настроек одним файлом, резервная копия в выбранную папку.

const exportFormat = "erised-switcher-settings"

type exportFile struct {
	Format  string          `json:"format"`
	Version string          `json:"version"`
	Created time.Time       `json:"created"`
	Config  *config.Config  `json:"config"`
	Words   words.Data      `json:"words"`
	History json.RawMessage `json:"typing,omitempty"`
}

// ExportSettings — настройки, словари и (по желанию) история набора одним JSON.
func (a *App) ExportSettings(withHistory bool) ([]byte, error) {
	f := exportFile{Format: exportFormat, Version: Version, Created: time.Now(), Config: a.cfg.Load(), Words: a.store.Snapshot()}
	if withHistory && a.hist != nil {
		b, err := a.hist.MarshalJSON()
		if err != nil {
			return nil, err
		}
		f.History = b
	}
	return json.MarshalIndent(f, "", " ")
}

// ImportSettings применяет файл экспорта: настройки и словари заменяются, история добавляется.
func (a *App) ImportSettings(b []byte) error {
	var f exportFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("это не файл настроек Erised Switcher: %w", err)
	}
	if f.Format != exportFormat || f.Config == nil {
		return errors.New("это не файл настроек Erised Switcher")
	}
	cur := a.cfg.Load()
	c := config.Default()
	cb, _ := json.Marshal(f.Config)
	if err := json.Unmarshal(cb, c); err != nil {
		return err
	}
	// Что относится к этому компьютеру, оставляем как есть.
	c.Autostart, c.InstallPrompted, c.WizardDone, c.UIPort = cur.Autostart, cur.InstallPrompted, true, cur.UIPort
	if errs := c.Validate(); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	if err := a.applyConfig(c, true); err != nil {
		return err
	}
	a.store.SetData(f.Words)
	if err := a.SaveWords(); err != nil {
		return err
	}
	if len(f.History) > 0 && a.hist != nil {
		if err := a.hist.Merge(f.History); err != nil {
			return fmt.Errorf("история набора: %w", err)
		}
		_ = a.hist.SaveIfDirty()
	}
	log.Printf("импортированы настройки из версии %s от %s", f.Version, f.Created.Format("2006-01-02"))
	return nil
}

var backupMu sync.Mutex

// backupNow пишет резервную копию в выбранную папку (вызывается после сохранений, не чаще раза в 10 с).
func (a *App) backupNow(force bool) error {
	dir := strings.TrimSpace(a.cfg.Load().BackupDir)
	if dir == "" {
		return nil
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	if !force && time.Since(a.lastBackup) < 10*time.Second {
		return nil
	}
	a.lastBackup = time.Now()
	b, err := a.ExportSettings(true)
	if err != nil {
		return err
	}
	// Пишем только если что-то изменилось (без даты в сравнении) — иначе облако синхронизировало бы
	// файл каждые 10 секунд.
	var probe exportFile
	_ = json.Unmarshal(b, &probe)
	probe.Created = time.Time{}
	pb, _ := json.Marshal(probe)
	sum := sha256.Sum256(append(pb, []byte(dir)...))
	if !force && sum == a.lastBackupSum {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := config.WriteAtomic(filepath.Join(dir, "erised-settings.json"), b); err != nil {
		return err
	}
	a.lastBackupSum = sum
	return nil
}

// BackupNow — кнопка «Сохранить копию сейчас».
func (a *App) BackupNow() error {
	if strings.TrimSpace(a.cfg.Load().BackupDir) == "" {
		return errors.New("сначала выберите папку для резервной копии")
	}
	return a.backupNow(true)
}

// PickFolder — стандартное окно выбора папки Windows (поверх окна настроек).
func (a *App) PickFolder() string {
	owner := win.FindWindowByTitle(settingswin.ClassName, settingswin.Title)
	return win.BrowseFolder(owner, "Папка для резервной копии настроек Erised Switcher (например, в OneDrive)")
}
