//go:build windows

package win

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// SetAutostart включает или выключает запуск вместе с Windows (для текущего пользователя).
func SetAutostart(name, exe string, on bool) error {
	var key uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(UTF16(runKey))), 0, KEY_SET_VALUE, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return syscall.Errno(r)
	}
	defer pRegCloseKey.Call(key)
	if !on {
		r, _, _ = pRegDeleteValueW.Call(key, uintptr(unsafe.Pointer(UTF16(name))))
		if r != 0 && r != 2 { // 2 — значения и так нет
			return syscall.Errno(r)
		}
		return nil
	}
	val := utf16.Encode([]rune(`"` + exe + `"`))
	val = append(val, 0)
	r, _, _ = pRegSetValueExW.Call(key, uintptr(unsafe.Pointer(UTF16(name))), 0, REG_SZ,
		uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// AutostartEnabled — есть ли запись автозапуска.
func AutostartEnabled(name string) bool {
	var key uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(UTF16(runKey))), 0, KEY_READ, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return false
	}
	defer pRegCloseKey.Call(key)
	r, _, _ = pRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(UTF16(name))), 0, 0, 0, 0)
	return r == 0
}
