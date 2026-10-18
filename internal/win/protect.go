//go:build windows

package win

import "runtime/debug"

// PanicHandler получает сбои, перехваченные Protect (программа записывает отчёт).
var PanicHandler func(where string, v any, stack []byte)

// Protect выполняет f и перехватывает сбой внутри. Нужен в обработчиках, которые вызывает
// Windows (перехват клавиш, окна): необработанный сбой там закрыл бы всю программу.
// Возвращает true, если был сбой.
func Protect(where string, f func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			if h := PanicHandler; h != nil {
				func() {
					defer func() { _ = recover() }()
					h(where, r, debug.Stack())
				}()
			}
		}
	}()
	f()
	return false
}
