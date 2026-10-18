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
	os.Exit(app.Run())
}
