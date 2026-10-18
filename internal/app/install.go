//go:build windows

package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"erised/internal/config"
	"erised/internal/win"
)

// Установка «для себя»: без прав администратора, в %LOCALAPPDATA%\Programs, с ярлыком в «Пуске»
// и записью в «Параметры → Приложения» (оттуда же удаление).
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ErisedSwitcher`

func installDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "Programs", "Erised Switcher")
}

func installedExePath() string { return filepath.Join(installDir(), "ErisedSwitcher.exe") }

func startMenuShortcut() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, "Erised Switcher.lnk")
}

func samePath(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

func selfExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

// isInstalledCopy — запущена установленная копия.
func isInstalledCopy() bool { return samePath(selfExe(), installedExePath()) }

// wingetManaged — программу установил winget.
func wingetManaged() bool {
	exe := strings.ToLower(selfExe())
	return strings.Contains(exe, `\winget\packages\`) || strings.Contains(exe, `\winget\links\`)
}

// managedElsewhere — программу поставил winget или она переносная: установку не предлагаем.
func managedElsewhere() bool {
	exe := strings.ToLower(selfExe())
	if strings.Contains(exe, `\winget\packages\`) || strings.Contains(exe, `\winget\links\`) {
		return true
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(selfExe()), "portable.txt")); err == nil {
		return true
	}
	return false
}

// InstallStatus — для окна настроек.
func (a *App) InstallStatus() map[string]any {
	return map[string]any{
		"installed":   win.RegKeyExistsHKCU(uninstallKey) && fileExists(installedExePath()),
		"this_copy":   isInstalledCopy(),
		"managed":     managedElsewhere(),
		"install_dir": redact(installDir()),
		"exe":         redact(selfExe()),
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// InstallAction — кнопки «Установить» и «Удалить программу».
func (a *App) InstallAction(op string) error {
	switch op {
	case "install":
		dst, err := installTo()
		if err != nil {
			return err
		}
		a.restartAs(dst)
	case "uninstall":
		exe := installedExePath()
		if !fileExists(exe) {
			exe = selfExe()
		}
		return exec.Command(exe, "--uninstall").Start()
	default:
		return fmt.Errorf("неизвестное действие %q", op)
	}
	return nil
}

// restartAs запускает другую копию программы и завершает эту.
func (a *App) restartAs(exe string) {
	_ = a.SaveWords()
	if a.hist != nil {
		_ = a.hist.SaveIfDirty()
	}
	if err := exec.Command(exe, "--after-install").Start(); err != nil {
		a.tray.Balloon("Не удалось запустить установленную копию", err.Error(), true)
		return
	}
	win.PostMessage(a.hwnd, win.WM_CLOSE, 0, 0)
}

// installTo копирует программу в папку установки, создаёт ярлык и запись для удаления.
func installTo() (string, error) {
	src := selfExe()
	if src == "" {
		return "", errors.New("не удалось определить, где лежит программа")
	}
	dst := installedExePath()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if !samePath(src, dst) {
		if err := replaceFile(src, dst); err != nil {
			return "", fmt.Errorf("копирование в %s: %w", filepath.Dir(dst), err)
		}
	}
	if err := win.CreateShortcut(startMenuShortcut(), dst, "", "Erised Switcher — исправление раскладки и опечаток", filepath.Dir(dst)); err != nil {
		log.Printf("ярлык: %v", err)
	}
	if err := registerUninstall(dst); err != nil {
		log.Printf("запись для удаления: %v", err)
	}
	win.NotifyShell()
	log.Printf("установлено в %s", dst)
	return dst, nil
}

// registerUninstall — запись в «Параметры → Приложения» (обновляется при каждом запуске установленной копии).
func registerUninstall(exe string) error {
	size := uint32(0)
	if fi, err := os.Stat(exe); err == nil {
		size = uint32(fi.Size() / 1024)
	}
	q := `"` + exe + `"`
	return win.RegWriteHKCU(uninstallKey, []win.RegValue{
		{Name: "DisplayName", Str: AppName},
		{Name: "DisplayVersion", Str: Version},
		{Name: "Publisher", Str: "NickolasFleim"},
		{Name: "DisplayIcon", Str: exe},
		{Name: "InstallLocation", Str: filepath.Dir(exe)},
		{Name: "UninstallString", Str: q + " --uninstall"},
		{Name: "QuietUninstallString", Str: q + " --uninstall --quiet"},
		{Name: "URLInfoAbout", Str: "https://github.com/NickolasFleim/ErisedSwitcher"},
		{Name: "HelpLink", Str: "https://github.com/NickolasFleim/ErisedSwitcher/issues"},
		{Name: "NoModify", Num: 1, IsNum: true},
		{Name: "NoRepair", Num: 1, IsNum: true},
		{Name: "EstimatedSize", Num: size, IsNum: true},
	})
}

// offerInstall — при первом запуске не из папки установки предложить установку.
// Возвращает true, если программа установлена и перезапускается оттуда (эту копию надо закрыть).
func (a *App) offerInstall(cfg *config.Config) bool {
	if isInstalledCopy() {
		if win.RegKeyExistsHKCU(uninstallKey) {
			_ = registerUninstall(selfExe()) // версия после обновления
		}
		return false
	}
	if cfg.InstallPrompted || managedElsewhere() {
		return false
	}
	// Ядро ещё не создано — сохраняем настройки напрямую, без applyConfig.
	c := cfg.Clone()
	c.InstallPrompted = true
	if err := config.Save(a.cfgPath, c); err == nil {
		a.cfg.Store(c)
		a.cfgMod = modTime(a.cfgPath)
	}
	const mbYesNo, mbQuestion, idYes = 0x4, 0x20, 6
	if win.MessageBox(AppName, "Установить Erised Switcher на этот компьютер?\n\n"+
		"Программа будет в «Пуске», обновляться сама и удаляться как обычное приложение "+
		"(Параметры → Приложения). Права администратора не нужны.\n\n"+
		"«Нет» — просто запустить из этой папки (спросим один раз).", mbYesNo|mbQuestion) != idYes {
		return false
	}
	dst, err := installTo()
	if err != nil {
		win.MessageBox(AppName, "Не удалось установить: "+err.Error()+"\n\nПрограмма запустится из текущей папки.", 0x30)
		return false
	}
	if cfg.Autostart {
		_ = win.SetAutostart(autostartName, dst, true)
	}
	if err := exec.Command(dst, "--after-install").Start(); err != nil {
		win.MessageBox(AppName, "Установлено, но не удалось запустить: "+err.Error(), 0x30)
		return false
	}
	return true
}

// RunUninstall — режим ErisedSwitcher.exe --uninstall [--quiet].
func RunUninstall(quiet bool) int {
	const mbYesNo, mbQuestion, idYes = 0x4, 0x20, 6
	if !quiet && win.MessageBox(AppName, "Удалить Erised Switcher с этого компьютера?", mbYesNo|mbQuestion) != idYes {
		return 0
	}
	// Закрыть работающую копию.
	if h := win.FindWindow(className); h != 0 {
		win.PostMessage(h, win.WM_CLOSE, 0, 0)
		for i := 0; i < 50 && win.FindWindow(className) != 0; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = win.SetAutostart(autostartName, "", false)
	_ = os.Remove(startMenuShortcut())
	win.RegDeleteTreeHKCU(uninstallKey)
	win.NotifyShell()
	dataDir := config.Dir()
	removeData := false
	if !quiet && win.MessageBox(AppName, "Удалить и настройки, словари, историю набора?\n\n"+redact(dataDir)+
		"\n\n«Нет» — оставить их на случай повторной установки.", mbYesNo|mbQuestion) == idYes {
		removeData = true
	}
	// Удалить папку программы, когда этот процесс завершится: запускаем копию из временной папки.
	dir := installDir()
	if fileExists(dir) {
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("ErisedSwitcher-uninstall-%d.exe", os.Getpid()))
		if err := copyFileMode(selfExe(), tmp); err == nil {
			args := []string{"--uninstall-cleanup", dir, strconv.Itoa(os.Getpid())}
			if removeData {
				args = append(args, dataDir)
			}
			_ = exec.Command(tmp, args...).Start()
		}
	} else if removeData {
		_ = os.RemoveAll(dataDir)
	}
	if !quiet {
		win.MessageBox(AppName, "Erised Switcher удалён.", 0x40)
	}
	return 0
}

// RunUninstallCleanup — ждёт завершения удаления и стирает папки (запускается из временной копии).
func RunUninstallCleanup(args []string) int {
	if len(args) < 2 {
		return 2
	}
	pid, _ := strconv.Atoi(args[1])
	win.WaitProcessExit(pid, 20*time.Second)
	targets := []string{args[0]}
	if len(args) > 2 {
		targets = append(targets, args[2])
	}
	for _, t := range targets {
		// Удаляем только наши папки — защита от ошибок в аргументах.
		if !strings.Contains(strings.ToLower(t), "erised switcher") {
			continue
		}
		for i := 0; i < 20; i++ {
			if err := os.RemoveAll(t); err == nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	return 0
}
