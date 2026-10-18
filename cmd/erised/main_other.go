//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "Erised Switcher работает только в Windows. Соберите: GOOS=windows GOARCH=amd64 go build -ldflags \"-H windowsgui\" -o ErisedSwitcher.exe ./cmd/erised")
	os.Exit(1)
}
