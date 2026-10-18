//go:build windows

package win

import (
	"errors"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	pRegDeleteTreeW   = advapi32.NewProc("RegDeleteTreeW")
	pSHChangeNotify   = shell32.NewProc("SHChangeNotify")
	clsidShellLink    = GUID{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW    = GUID{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile   = GUID{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	errShortcut       = errors.New("не удалось создать ярлык")
)

func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*[32]uintptr)(uptr(*(*uintptr)(uptr(obj))))
	r, _, _ := syscall.SyscallN(vtbl[idx], append([]uintptr{obj}, args...)...)
	return r
}

// CreateShortcut создаёт ярлык .lnk (IShellLink).
func CreateShortcut(lnk, target, args, description, workDir string) error {
	pCoInitializeEx.Call(0, 2) // в этом потоке COM может быть уже инициализирован — это не ошибка
	var sl uintptr
	if r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1, /*CLSCTX_INPROC_SERVER*/
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&sl))); r != 0 || sl == 0 {
		return errShortcut
	}
	defer comCall(sl, 2) // Release
	pTarget, pArgs, pDesc, pDir := UTF16(target), UTF16(args), UTF16(description), UTF16(workDir)
	comCall(sl, 20, uintptr(unsafe.Pointer(pTarget)))    // SetPath
	comCall(sl, 11, uintptr(unsafe.Pointer(pArgs)))      // SetArguments
	comCall(sl, 7, uintptr(unsafe.Pointer(pDesc)))       // SetDescription
	comCall(sl, 9, uintptr(unsafe.Pointer(pDir)))        // SetWorkingDirectory
	comCall(sl, 17, uintptr(unsafe.Pointer(pTarget)), 0) // SetIconLocation
	runtime.KeepAlive(pArgs)
	runtime.KeepAlive(pDesc)
	runtime.KeepAlive(pDir)
	var pf uintptr
	if r := comCall(sl, 0, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pf))); r != 0 || pf == 0 {
		return errShortcut
	}
	defer comCall(pf, 2)
	pLnk := UTF16(lnk)
	r := comCall(pf, 6, uintptr(unsafe.Pointer(pLnk)), 1) // Save
	runtime.KeepAlive(pLnk)
	runtime.KeepAlive(pTarget)
	if r != 0 {
		return errShortcut
	}
	return nil
}

// RegValue — значение реестра: строка или число.
type RegValue struct {
	Name  string
	Str   string
	Num   uint32
	IsNum bool
}

// RegWriteHKCU создаёт ключ HKCU\path и записывает значения.
func RegWriteHKCU(path string, values []RegValue) error {
	var key uintptr
	const keyAllAccess = 0xF003F
	if r, _, _ := pRegCreateKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(UTF16(path))), 0, 0, 0, keyAllAccess, 0,
		uintptr(unsafe.Pointer(&key)), 0); r != 0 {
		return syscall.Errno(r)
	}
	defer pRegCloseKey.Call(key)
	for _, v := range values {
		var r uintptr
		if v.IsNum {
			n := v.Num
			r, _, _ = pRegSetValueExW.Call(key, uintptr(unsafe.Pointer(UTF16(v.Name))), 0, 4 /*REG_DWORD*/, uintptr(unsafe.Pointer(&n)), 4)
		} else {
			u := append(utf16.Encode([]rune(v.Str)), 0)
			r, _, _ = pRegSetValueExW.Call(key, uintptr(unsafe.Pointer(UTF16(v.Name))), 0, REG_SZ, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)*2))
		}
		if r != 0 {
			return syscall.Errno(r)
		}
	}
	return nil
}

// RegDeleteTreeHKCU удаляет ключ HKCU\path со всем содержимым.
func RegDeleteTreeHKCU(path string) {
	pRegDeleteTreeW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(UTF16(path))))
}

// RegKeyExistsHKCU — есть ли ключ.
func RegKeyExistsHKCU(path string) bool {
	var key uintptr
	if r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(UTF16(path))), 0, KEY_READ, uintptr(unsafe.Pointer(&key))); r != 0 {
		return false
	}
	pRegCloseKey.Call(key)
	return true
}

// NotifyShell — сообщить проводнику, что ярлыки изменились.
func NotifyShell() { pSHChangeNotify.Call(0x08000000 /*SHCNE_ASSOCCHANGED*/, 0, 0, 0) }
