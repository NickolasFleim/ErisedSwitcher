//go:build windows

// Package settingswin — окно настроек как отдельное окно программы (WebView2).
// Запускается отдельным процессом (ErisedSwitcher.exe --settings): если WebView2 в системе
// не работает, это не затронет перехват клавиш в основном процессе.
package settingswin

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/jchv/go-webview2"

	"erised/internal/trayicon"
	"erised/internal/win"
)

// Title — заголовок окна (по нему основной процесс находит уже открытое окно).
const Title = "Erised Switcher — настройки"

// ClassName — класс окна, который создаёт WebView2-обёртка.
const ClassName = "webview"

// ExitNoWebView — код выхода, если WebView2 недоступен (основной процесс откроет запасной вариант).
const ExitNoWebView = 3

// Run показывает окно и возвращает код выхода процесса.
func Run(url, dataDir string) int {
	runtime.LockOSThread()
	win.EnableDPIAwareness()
	if !win.SingleInstance(`Local\ErisedSwitcherSettings`) {
		if h := win.FindWindowByTitle(ClassName, Title); h != 0 {
			win.BringToFront(h)
		}
		return 0
	}
	if f, err := os.OpenFile(filepath.Join(dataDir, "erised-settings.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	scale := float64(win.SystemDPI()) / 96
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(dataDir, "webview2"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  Title,
			Width:  uint(1140 * scale),
			Height: uint(820 * scale),
			Center: true,
		},
	})
	if w == nil {
		log.Printf("WebView2 недоступен")
		return ExitNoWebView
	}
	hwnd := uintptr(w.Window())
	w.SetSize(int(760*scale), int(520*scale), webview2.HintMin)
	big := win.IconFromBGRA(int(32*scale), trayicon.RenderMirror(int(32*scale), false))
	small := win.IconFromBGRA(int(16*scale), trayicon.RenderMirror(int(16*scale), false))
	win.SetWindowIcon(hwnd, big, small)

	// Страница сообщает, есть ли несохранённые изменения; при закрытии окна спрашиваем.
	var dirty, closing atomic.Bool
	_ = w.Bind("erisedSetDirty", func(v bool) { dirty.Store(v) })
	_ = w.Bind("erisedClose", func() {
		closing.Store(true)
		win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
	})
	win.Subclass(hwnd, func(h uintptr, msg uint32, wp, lp uintptr, next func() uintptr) uintptr {
		if msg == win.WM_CLOSE && dirty.Load() && !closing.Load() {
			const mbYesNoCancel, mbIconQuestion = 0x3, 0x20
			switch win.MessageBoxOwned(h, Title, "Сохранить изменения в настройках?", mbYesNoCancel|mbIconQuestion) {
			case 6: // Да: сохранит страница и сама закроет окно, если сохранение прошло
				w.Eval("window.erisedSaveAndClose && window.erisedSaveAndClose()")
				return 0
			case 7: // Нет
				closing.Store(true)
			default: // Отмена
				return 0
			}
		}
		return next()
	})
	w.Navigate(url)
	w.Run()
	win.DestroyIcon(big)
	win.DestroyIcon(small)
	return 0
}
