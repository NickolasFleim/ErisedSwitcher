//go:build windows

package win

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	pCreateCompatibleDC   = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBmp  = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject         = gdi32.NewProc("SelectObject")
	pDeleteDC             = gdi32.NewProc("DeleteDC")
	pBitBlt               = gdi32.NewProc("BitBlt")
	pCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	pCreateFontW          = gdi32.NewProc("CreateFontW")
	pSetTextColor         = gdi32.NewProc("SetTextColor")
	pSetBkMode            = gdi32.NewProc("SetBkMode")
	pGetTextExtentPoint32 = gdi32.NewProc("GetTextExtentPoint32W")
	pTextOutW             = gdi32.NewProc("TextOutW")

	pBeginPaint        = user32.NewProc("BeginPaint")
	pEndPaint          = user32.NewProc("EndPaint")
	pFillRect          = user32.NewProc("FillRect")
	pInvalidateRect    = user32.NewProc("InvalidateRect")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pGetDC             = user32.NewProc("GetDC")
	pReleaseDC         = user32.NewProc("ReleaseDC")
	pClientToScreen    = user32.NewProc("ClientToScreen")
	pMonitorFromPoint  = user32.NewProc("MonitorFromPoint")
	pMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	pTrackMouseEvent   = user32.NewProc("TrackMouseEvent")
	pLoadCursorW       = user32.NewProc("LoadCursorW")
	pSetCursor         = user32.NewProc("SetCursor")

	shcore            = syscall.NewLazyDLL("shcore.dll")
	pGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")
	dwmapi            = syscall.NewLazyDLL("dwmapi.dll")
	pDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
	oleacc            = syscall.NewLazyDLL("oleacc.dll")
	pAccObjFromWindow = oleacc.NewProc("AccessibleObjectFromWindow")
	ole32             = syscall.NewLazyDLL("ole32.dll")
	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
)

// PopupItem — вариант в полоске.
type PopupItem struct {
	Text string
	Hint string // подпись клавиши (может быть пустой)
	Bold bool   // будет применён сам (исправление опечатки)
}

// PopupState — что и где показать.
type PopupState struct {
	Items   []PopupItem
	Version uint64
	// Окно, в которое идёт ввод, и его поток (для поиска курсора ввода).
	Target, Focus uintptr
	Thread        uint32
	Position      string // caret | mouse | bottom
	Fallback      string // mouse | bottom
	UseAcc        bool
	Scale         float64
	Clickable     bool
}

// Popup — полоска подсказок: окно без фокуса поверх всех окон. Работает в своём потоке.
type Popup struct {
	OnClick func(idx int, version uint64)

	hwnd    uintptr
	ready   chan struct{}
	mu      sync.Mutex
	state   PopupState
	visible bool
	pending atomic.Bool
	hover   int
	cells   []RECT // в координатах окна
	rect    atomic.Pointer[RECT]

	dpi                int
	fontMain, fontBold uintptr
	fontHint           uintptr
	dark               bool
	themeChecked       time.Time
	lastCaret          POINT
	lastCaretOK        bool
	lastCaretFor       uintptr
}

const (
	wmPopupUpdate = WM_APP + 40
	wmPopupHide   = WM_APP + 41
)

// NewPopup создаёт полоску (окно появится при первом Show).
func NewPopup(onClick func(idx int, version uint64)) *Popup {
	p := &Popup{OnClick: onClick, ready: make(chan struct{}), hover: -1}
	go p.loop()
	<-p.ready
	return p
}

// Show показывает варианты (вызывать можно из любого потока; применяется последнее состояние).
func (p *Popup) Show(s PopupState) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
	if !p.pending.Swap(true) {
		PostMessage(p.hwnd, wmPopupUpdate, 0, 0)
	}
}

// Hide прячет полоску.
func (p *Popup) Hide() {
	p.mu.Lock()
	p.state.Items = nil
	p.mu.Unlock()
	if !p.pending.Swap(true) {
		PostMessage(p.hwnd, wmPopupUpdate, 0, 0)
	}
}

// Contains — точка экрана над полоской (щелчок по ней не должен сбрасывать набранное слово).
func (p *Popup) Contains(pt POINT) bool {
	r := p.rect.Load()
	return r != nil && pt.X >= r.Left && pt.X < r.Right && pt.Y >= r.Top && pt.Y < r.Bottom
}

// Visible — показана ли полоска.
func (p *Popup) Visible() bool { return p.rect.Load() != nil }

func (p *Popup) loop() {
	runtime.LockOSThread()
	pCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED — для специальных возможностей
	hinst, _, _ := pGetModuleHandleW.Call(0)
	cls := "ErisedSuggestPopup"
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0x00020000, // CS_DROPSHADOW
		LpfnWndProc:   syscall.NewCallback(p.wndProc),
		HInstance:     hinst,
		LpszClassName: UTF16(cls),
	}
	wc.HCursor, _, _ = pLoadCursorW.Call(0, 32649) // IDC_HAND
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	const (
		wsPopup        = 0x80000000
		wsExTopmost    = 0x00000008
		wsExToolWindow = 0x00000080
		wsExNoActivate = 0x08000000
	)
	p.hwnd, _, _ = pCreateWindowExW.Call(wsExTopmost|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(UTF16(cls))), 0, wsPopup, 0, 0, 10, 10, 0, 0, hinst, 0)
	pref := uint32(2) // DWMWCP_ROUND — скругления в Windows 11
	pDwmSetWindowAttr.Call(p.hwnd, 33, uintptr(unsafe.Pointer(&pref)), 4)
	close(p.ready)
	MessageLoop()
}

func (p *Popup) wndProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmPopupUpdate:
		p.pending.Store(false)
		p.update()
		return 0
	case 0x0021: // WM_MOUSEACTIVATE
		return 3 // MA_NOACTIVATE: щелчок не забирает фокус у окна, где вы печатаете
	case WM_LBUTTONDOWN:
		x := int32(int16(l & 0xFFFF))
		p.mu.Lock()
		ver, click := p.state.Version, p.state.Clickable
		p.mu.Unlock()
		if click {
			for i, c := range p.cells {
				if x >= c.Left && x < c.Right {
					if p.OnClick != nil {
						go p.OnClick(i, ver)
					}
					break
				}
			}
		}
		return 0
	case 0x0200: // WM_MOUSEMOVE
		x := int32(int16(l & 0xFFFF))
		h := -1
		for i, c := range p.cells {
			if x >= c.Left && x < c.Right {
				h = i
			}
		}
		if h != p.hover {
			p.hover = h
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		tme := struct {
			Size, Flags uint32
			Hwnd        uintptr
			Hover       uint32
		}{24, 2, hwnd, 0} // TME_LEAVE
		pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
		return 0
	case 0x02A3: // WM_MOUSELEAVE
		p.hover = -1
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case 0x000F: // WM_PAINT
		p.paint(hwnd)
		return 0
	case 0x0014: // WM_ERASEBKGND
		return 1
	}
	return DefWindowProc(hwnd, msg, w, l)
}

func (p *Popup) hideNow() {
	if p.visible {
		pShowWindow.Call(p.hwnd, 0) // SW_HIDE
		p.visible = false
	}
	p.rect.Store(nil)
	p.hover = -1
}

func (p *Popup) update() {
	p.mu.Lock()
	s := p.state
	s.Items = append([]PopupItem(nil), p.state.Items...)
	p.mu.Unlock()
	if len(s.Items) == 0 || s.Position == "off" {
		p.hideNow()
		return
	}
	anchor, below, ok := p.anchor(&s)
	if !ok {
		p.hideNow()
		return
	}
	p.checkTheme()
	dpi := monitorDPI(anchor)
	if s.Scale <= 0 {
		s.Scale = 1
	}
	p.ensureFonts(dpi, s.Scale)
	px := func(v float64) int32 { return int32(v*float64(dpi)/96*s.Scale + 0.5) }

	// Размеры ячеек.
	dc, _, _ := pGetDC.Call(p.hwnd)
	padX, gapHint := px(12), px(6)
	height := px(30)
	x := int32(0)
	p.cells = p.cells[:0]
	for _, it := range s.Items {
		f := p.fontMain
		if it.Bold {
			f = p.fontBold
		}
		tw := textWidth(dc, f, it.Text)
		if it.Hint != "" {
			tw += gapHint + textWidth(dc, p.fontHint, it.Hint)
		}
		wc := tw + 2*padX
		if wc < px(56) {
			wc = px(56)
		}
		p.cells = append(p.cells, RECT{Left: x, Top: 0, Right: x + wc, Bottom: height})
		x += wc
	}
	pReleaseDC.Call(p.hwnd, dc)
	width := x
	p.mu.Lock()
	p.state.Items = s.Items // для отрисовки
	p.mu.Unlock()

	// Положение: под курсором ввода, а если не помещается — над ним; в пределах рабочей области.
	work := workArea(anchor)
	var posX, posY int32
	if s.Position == "bottom" {
		posX = (work.Left+work.Right)/2 - width/2
		posY = work.Bottom - height - px(12)
	} else {
		posX = anchor.X - padX
		posY = anchor.Y + px(6)
		if !below {
			posY = anchor.Y + px(20)
		}
	}
	if posX+width > work.Right {
		posX = work.Right - width
	}
	if posX < work.Left {
		posX = work.Left
	}
	if posY+height > work.Bottom {
		posY = anchor.Y - height - px(28)
		if posY < work.Top {
			posY = work.Bottom - height
		}
	}
	const swpNoActivate, swpShow = 0x0010, 0x0040
	pSetWindowPos.Call(p.hwnd, ^uintptr(0) /*HWND_TOPMOST*/, uintptr(posX), uintptr(posY), uintptr(width), uintptr(height),
		swpNoActivate|swpShow)
	p.visible = true
	p.rect.Store(&RECT{Left: posX, Top: posY, Right: posX + width, Bottom: posY + height})
	pInvalidateRect.Call(p.hwnd, 0, 0)
}

// anchor находит, где показывать полоску. below=true — координата низа курсора ввода.
func (p *Popup) anchor(s *PopupState) (pt POINT, below bool, ok bool) {
	mouse := func() (POINT, bool, bool) {
		var c POINT
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&c)))
		return POINT{c.X + 4, c.Y + 4}, false, true
	}
	bottomOf := func() (POINT, bool, bool) {
		// Центр монитора, где окно ввода.
		var r RECT
		mon, _, _ := pMonitorFromWindow.Call(s.Target, 2)
		if mi, okm := monitorInfo(mon); okm {
			r = mi
		}
		return POINT{(r.Left + r.Right) / 2, (r.Top + r.Bottom) / 2}, false, true
	}
	switch s.Position {
	case "mouse":
		return mouse()
	case "bottom":
		pt, _, _ := bottomOf()
		return pt, false, true
	}
	if c, okc := caretPos(s.Thread, s.Focus, s.UseAcc); okc {
		return c, true, true
	}
	if s.Fallback == "bottom" {
		pt, _, _ := bottomOf()
		s.Position = "bottom"
		return pt, false, true
	}
	return mouse()
}

// caretPos — низ-лево курсора ввода в координатах экрана.
func caretPos(tid uint32, focus uintptr, useAcc bool) (POINT, bool) {
	if gi, ok := GUIThreadInfo(tid); ok && gi.HwndCaret != 0 && (gi.RcCaret.Bottom > gi.RcCaret.Top || gi.RcCaret.Right > gi.RcCaret.Left) {
		pt := POINT{gi.RcCaret.Left, gi.RcCaret.Bottom}
		pClientToScreen.Call(gi.HwndCaret, uintptr(unsafe.Pointer(&pt)))
		return pt, true
	}
	if useAcc && focus != 0 {
		if x, y, w, h, ok := accCaret(focus); ok && (w > 0 || h > 0) && (x != 0 || y != 0) {
			return POINT{x, y + h}, true
		}
	}
	return POINT{}, false
}

var iidIAccessible = GUID{0x618736E0, 0x3C3D, 0x11CF, [8]byte{0x81, 0x0C, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}

// accCaret спрашивает положение курсора ввода через специальные возможности (MSAA, OBJID_CARET).
// Так курсор находится в Chrome, Edge, Firefox, Office и многих других программах.
func accCaret(hwnd uintptr) (x, y, w, h int32, ok bool) {
	if pAccObjFromWindow.Find() != nil {
		return 0, 0, 0, 0, false
	}
	var acc uintptr
	const objidCaret = 0xFFFFFFF8
	r, _, _ := pAccObjFromWindow.Call(hwnd, objidCaret, uintptr(unsafe.Pointer(&iidIAccessible)), uintptr(unsafe.Pointer(&acc)))
	if r != 0 || acc == 0 {
		return 0, 0, 0, 0, false
	}
	vtbl := *(*[28]uintptr)(uptr(*(*uintptr)(uptr(acc))))
	defer syscall.SyscallN(vtbl[2], acc) // Release
	// VARIANT с VT_I4 = 0 (CHILDID_SELF). Больше 8/16 байт — передаётся по указателю.
	var variant [24]byte
	variant[0] = 3 // VT_I4
	var lx, ly, lw, lh int32
	hr, _, _ := syscall.SyscallN(vtbl[22], acc, uintptr(unsafe.Pointer(&lx)), uintptr(unsafe.Pointer(&ly)),
		uintptr(unsafe.Pointer(&lw)), uintptr(unsafe.Pointer(&lh)), uintptr(unsafe.Pointer(&variant[0])))
	if hr != 0 {
		return 0, 0, 0, 0, false
	}
	return lx, ly, lw, lh, true
}

func monitorInfo(mon uintptr) (RECT, bool) {
	var mi struct {
		Size          uint32
		Monitor, Work RECT
		Flags         uint32
	}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return RECT{}, false
	}
	return mi.Work, true
}

func workArea(pt POINT) RECT {
	mon, _, _ := pMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, 2)
	if r, ok := monitorInfo(mon); ok {
		return r
	}
	return RECT{0, 0, 1920, 1080}
}

func monitorDPI(pt POINT) int {
	if pGetDpiForMonitor.Find() == nil {
		mon, _, _ := pMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, 2)
		var dx, dy uint32
		if r, _, _ := pGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
			return int(dx)
		}
	}
	return SystemDPI()
}

func (p *Popup) ensureFonts(dpi int, scale float64) {
	key := int(float64(dpi) * scale * 100)
	if p.dpi == key && p.fontMain != 0 {
		return
	}
	for _, f := range []uintptr{p.fontMain, p.fontBold, p.fontHint} {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
	mk := func(pt float64, weight int) uintptr {
		h := -int32(pt*float64(dpi)/72*scale + 0.5)
		f, _, _ := pCreateFontW.Call(uintptr(h), 0, 0, 0, uintptr(weight), 0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE*/, 0,
			uintptr(unsafe.Pointer(UTF16("Segoe UI"))))
		return f
	}
	p.fontMain, p.fontBold, p.fontHint = mk(10.5, 400), mk(10.5, 700), mk(8, 400)
	p.dpi = key
}

func textWidth(dc, font uintptr, s string) int32 {
	old, _, _ := pSelectObject.Call(dc, font)
	u, _ := syscall.UTF16FromString(s)
	var sz struct{ CX, CY int32 }
	if len(u) > 1 {
		pGetTextExtentPoint32.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	}
	pSelectObject.Call(dc, old)
	return sz.CX
}

func rgb(r, g, b uint8) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

// checkTheme — светлая или тёмная тема приложений (раз в 5 секунд).
func (p *Popup) checkTheme() {
	if time.Since(p.themeChecked) < 5*time.Second {
		return
	}
	p.themeChecked = time.Now()
	p.dark = !regDWORD(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "AppsUseLightTheme", 1)
}

func regDWORD(path, name string, def uint32) bool {
	var key uintptr
	if r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(UTF16(path))), 0, KEY_READ, uintptr(unsafe.Pointer(&key))); r != 0 {
		return def != 0
	}
	defer pRegCloseKey.Call(key)
	var v, size, typ uint32 = 0, 4, 0
	if r, _, _ := pRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(UTF16(name))), 0, uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size))); r != 0 {
		return def != 0
	}
	return v != 0
}

func (p *Popup) paint(hwnd uintptr) {
	var ps [72]byte // PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps[0])))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps[0])))
	r := p.rect.Load()
	if r == nil {
		return
	}
	w, h := r.Right-r.Left, r.Bottom-r.Top
	mem, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBmp.Call(hdc, uintptr(w), uintptr(h))
	oldBmp, _, _ := pSelectObject.Call(mem, bmp)

	bg, fg, muted, line, hover, accent := rgb(0xFF, 0xFF, 0xFF), rgb(0x1B, 0x21, 0x30), rgb(0x6B, 0x72, 0x80), rgb(0xE3, 0xE6, 0xEC), rgb(0xEE, 0xF2, 0xFA), rgb(0x25, 0x63, 0xEB)
	if p.dark {
		bg, fg, muted, line, hover, accent = rgb(0x27, 0x2D, 0x39), rgb(0xE7, 0xEA, 0xF0), rgb(0x9A, 0xA3, 0xB5), rgb(0x3A, 0x42, 0x52), rgb(0x33, 0x3B, 0x4B), rgb(0x6B, 0x9B, 0xFF)
	}
	fill := func(rc RECT, c uintptr) {
		br, _, _ := pCreateSolidBrush.Call(c)
		pFillRect.Call(mem, uintptr(unsafe.Pointer(&rc)), br)
		pDeleteObject.Call(br)
	}
	fill(RECT{0, 0, w, h}, bg)
	p.mu.Lock()
	items := p.state.Items
	p.mu.Unlock()
	pSetBkMode.Call(mem, 1) // TRANSPARENT
	scale := float64(h) / 30
	for i, c := range p.cells {
		if i >= len(items) {
			break
		}
		it := items[i]
		if i == p.hover {
			fill(c, hover)
		}
		if i > 0 {
			fill(RECT{c.Left, int32(6 * scale), c.Left + 1, h - int32(6*scale)}, line)
		}
		f := p.fontMain
		if it.Bold {
			f = p.fontBold
		}
		tw := textWidth(mem, f, it.Text)
		hw := int32(0)
		gap := int32(6 * scale)
		if it.Hint != "" {
			hw = gap + textWidth(mem, p.fontHint, it.Hint)
		}
		x := c.Left + (c.Right-c.Left-tw-hw)/2
		col := fg
		if it.Bold {
			col = accent
		}
		drawText(mem, f, col, x, (h-int32(19*scale))/2, it.Text)
		if it.Hint != "" {
			drawText(mem, p.fontHint, muted, x+tw+gap, (h-int32(13*scale))/2+int32(1*scale), it.Hint)
		}
		if it.Bold {
			fill(RECT{x, h - int32(5*scale), x + tw, h - int32(5*scale) + int32(2*scale+0.5)}, accent)
		}
	}
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, 0x00CC0020 /*SRCCOPY*/)
	pSelectObject.Call(mem, oldBmp)
	pDeleteObject.Call(bmp)
	pDeleteDC.Call(mem)
}

func drawText(dc, font, color uintptr, x, y int32, s string) {
	old, _, _ := pSelectObject.Call(dc, font)
	pSetTextColor.Call(dc, color)
	u, _ := syscall.UTF16FromString(s)
	if len(u) > 1 {
		pTextOutW.Call(dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
	}
	pSelectObject.Call(dc, old)
}
