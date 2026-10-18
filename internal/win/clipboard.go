//go:build windows

package win

import (
	"errors"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

func openClipboard(hwnd uintptr) error {
	for i := 0; i < 20; i++ {
		if r, _, _ := pOpenClipboard.Call(hwnd); r != 0 {
			return nil
		}
		time.Sleep(15 * time.Millisecond)
	}
	return errors.New("буфер обмена занят")
}

// ClipboardSeq — номер изменения буфера обмена.
func ClipboardSeq() uint32 {
	r, _, _ := pGetClipboardSequenceNumber.Call()
	return uint32(r)
}

// GetClipboardText читает текст из буфера обмена.
func GetClipboardText(hwnd uintptr) (string, bool) {
	if openClipboard(hwnd) != nil {
		return "", false
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return "", false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer pGlobalUnlock.Call(h)
	// Память получена от GlobalLock (не куча Go). Ищем завершающий ноль, не дальше 2 МБ символов.
	sz, _, _ := pGlobalSize.Call(h)
	max := int(sz / 2)
	if max <= 0 {
		return "", false
	}
	if max > 2<<20 {
		max = 2 << 20
	}
	buf := unsafe.Slice((*uint16)(uptr(p)), max)
	return syscall.UTF16ToString(buf), true
}

// SetClipboardText записывает текст в буфер обмена.
func SetClipboardText(hwnd uintptr, s string) error {
	if err := openClipboard(hwnd); err != nil {
		return err
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	u := append(utf16.Encode([]rune(s)), 0)
	size := uintptr(len(u) * 2)
	h, _, _ := pGlobalAlloc.Call(GMEM_MOVEABLE, size)
	if h == 0 {
		return errors.New("нет памяти")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return errors.New("нет памяти")
	}
	copy(unsafe.Slice((*uint16)(uptr(p)), len(u)), u)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboardData.Call(CF_UNICODETEXT, h); r == 0 {
		pGlobalFree.Call(h)
		return errors.New("не удалось записать в буфер обмена")
	}
	return nil
}

// uptr превращает адрес из Win32 API в unsafe.Pointer (память вне кучи Go).
func uptr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }
