//go:build windows

// Erised Switcher — автоматическое исправление раскладки RU/EN для Windows, в том числе в терминалах.
package main

import (
	"os"

	"erised/internal/app"
	"erised/internal/settingswin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--settings" {
		url, dir := os.Getenv("ERISED_UI_URL"), os.Getenv("ERISED_DATA_DIR")
		if url == "" || dir == "" {
			os.Exit(2)
		}
		os.Exit(settingswin.Run(url, dir))
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--uninstall":
			os.Exit(app.RunUninstall(len(os.Args) > 2 && os.Args[2] == "--quiet"))
		case "--uninstall-cleanup":
			os.Exit(app.RunUninstallCleanup(os.Args[2:]))
		case "--selftest":
			report := ""
			if len(os.Args) > 2 {
				report = os.Args[2]
			}
			os.Exit(app.SelfTest(report))
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "--apply-update" {
		os.Exit(app.ApplyUpdate(os.Args[2:]))
	}
	os.Exit(app.Run())
}
