//go:build windows

package win

import (
	"unsafe"
)

// CreateMessageWindow создаёт скрытое окно для трея и таймеров.
func CreateMessageWindow(className string, proc uintptr) uintptr {
	hinst, _, _ := pGetModuleHandleW.Call(0)
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   proc,
		HInstance:     hinst,
		LpszClassName: UTF16(className),
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	// Обычное (не HWND_MESSAGE) скрытое окно — иначе не придёт TaskbarCreated.
	h, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(UTF16(className))), uintptr(unsafe.Pointer(UTF16(className))),
		0, 0, 0, 0, 0, 0, 0, hinst, 0)
	return h
}

// DefWindowProc — обработка по умолчанию.
func DefWindowProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), w, l)
	return r
}

// DestroyWindow закрывает окно.
func DestroyWindow(hwnd uintptr) { pDestroyWindow.Call(hwnd) }

// PostQuitMessage завершает цикл сообщений.
func PostQuitMessage() { pPostQuitMessage.Call(0) }

// SetTimer запускает таймер окна.
func SetTimer(hwnd uintptr, id uintptr, ms uint32) { pSetTimer.Call(hwnd, id, uintptr(ms), 0) }

// RegisterWindowMessage — системное сообщение по имени.
func RegisterWindowMessage(name string) uint32 {
	r, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(UTF16(name))))
	return uint32(r)
}

// MessageLoop крутит цикл сообщений текущего потока до WM_QUIT.
func MessageLoop() {
	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// SmallIconSize — размер значка в трее с учётом масштаба экрана.
func SmallIconSize() int {
	r, _, _ := pGetSystemMetrics.Call(SM_CXSMICON)
	if r == 0 {
		return 16
	}
	return int(r)
}

// IconFromBGRA создаёт HICON из пикселей BGRA (сверху вниз, premultiplied alpha).
func IconFromBGRA(size int, px []byte) uintptr {
	bi := BITMAPINFOHEADER{
		BiSize:     uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:    int32(size),
		BiHeight:   -int32(size), // сверху вниз
		BiPlanes:   1,
		BiBitCount: 32,
	}
	var bits uintptr
	color, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 || bits == 0 {
		return 0
	}
	copy(unsafe.Slice((*byte)(uptr(bits)), len(px)), px)
	maskBits := make([]byte, ((size+15)/16)*2*size) // монохромная маска: всё нули, прозрачность из альфы
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	ii := ICONINFO{FIcon: 1, HbmMask: mask, HbmColor: color}
	h, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	pDeleteObject.Call(color)
	pDeleteObject.Call(mask)
	return h
}

// DestroyIcon освобождает значок.
func DestroyIcon(h uintptr) {
	if h != 0 {
		pDestroyIcon.Call(h)
	}
}

// Tray — значок в области уведомлений.
type Tray struct {
	Hwnd    uintptr
	ID      uint32
	CbMsg   uint32
	added   bool
	lastTip string
	icon    uintptr
}

func (t *Tray) data(flags uint32) NOTIFYICONDATAW {
	d := NOTIFYICONDATAW{CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})), HWnd: t.Hwnd, UID: t.ID, UFlags: flags, UCallbackMessage: t.CbMsg}
	return d
}

// Set показывает или обновляет значок и подсказку.
func (t *Tray) Set(icon uintptr, tip string) {
	d := t.data(NIF_MESSAGE | NIF_ICON | NIF_TIP)
	d.HIcon = icon
	copyUTF16(d.SzTip[:], tip)
	if !t.added {
		r, _, _ := pShell_NotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&d)))
		t.added = r != 0
	} else {
		r, _, _ := pShell_NotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&d)))
		if r == 0 { // проводник перезапускался — добавим заново
			r, _, _ = pShell_NotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&d)))
			t.added = r != 0
		}
	}
	t.icon, t.lastTip = icon, tip
}

// Readd — после перезапуска проводника (TaskbarCreated).
func (t *Tray) Readd() {
	t.added = false
	t.Set(t.icon, t.lastTip)
}

// Balloon показывает всплывающее уведомление.
func (t *Tray) Balloon(title, text string, warn bool) {
	d := t.data(NIF_INFO)
	copyUTF16(d.SzInfoTitle[:], title)
	copyUTF16(d.SzInfo[:], text)
	d.DwInfoFlags = NIIF_INFO
	if warn {
		d.DwInfoFlags = NIIF_WARNING
	}
	pShell_NotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&d)))
}

// Remove убирает значок.
func (t *Tray) Remove() {
	d := t.data(0)
	pShell_NotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&d)))
	t.added = false
}

// MenuItem — пункт меню (ID 0 — разделитель).
type MenuItem struct {
	ID      uint32
	Text    string
	Checked bool
	Grayed  bool
}

// PopupMenu показывает меню у курсора и возвращает выбранный ID (0 — ничего).
func PopupMenu(hwnd uintptr, items []MenuItem) uint32 {
	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	for _, it := range items {
		if it.ID == 0 {
			pAppendMenuW.Call(m, MF_SEPARATOR, 0, 0)
			continue
		}
		f := uint32(MF_STRING)
		if it.Checked {
			f |= MF_CHECKED
		}
		if it.Grayed {
			f |= MF_GRAYED
		}
		pAppendMenuW.Call(m, uintptr(f), uintptr(it.ID), uintptr(unsafe.Pointer(UTF16(it.Text))))
	}
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	r, _, _ := pTrackPopupMenu.Call(m, TPM_RETURNCMD|TPM_RIGHTBUTTON|TPM_BOTTOMALIGN, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	PostMessage(hwnd, 0, 0, 0) // WM_NULL — чтобы меню корректно закрывалось
	return uint32(r)
}
