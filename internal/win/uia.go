//go:build windows

package win

import (
	"runtime"
	"syscall"
	"time"
	"unicode"
	"unsafe"
)

// Чтение текста у курсора ввода через UI Automation (специальные возможности Windows).
// Работает в отдельном потоке: зависшая программа не должна задерживать перехват клавиш.

var (
	oleaut32       = syscall.NewLazyDLL("oleaut32.dll")
	pSysFreeString = oleaut32.NewProc("SysFreeString")
	pSysStringLen  = oleaut32.NewProc("SysStringLen")

	clsidCUIAutomation = GUID{0xFF48DBA4, 0x60EF, 0x4201, [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation   = GUID{0x30CBE57D, 0xD9D0, 0x452A, [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
	iidTextPattern     = GUID{0x32EBA289, 0x3583, 0x42C9, [8]byte{0x9C, 0x59, 0x3B, 0x6D, 0x9A, 0x1E, 0x9B, 0x6A}}
)

// CaretText — что прочитано у курсора ввода.
type CaretText struct {
	Before    string // текст перед курсором (до ~80 символов)
	AfterWord bool   // сразу за курсором буква или цифра — курсор посреди слова
}

type uiaReq struct {
	reply chan CaretText
	ok    chan bool
}

var (
	uiaQueue = make(chan uiaReq, 1)
	uiaStart = make(chan struct{}, 1)
)

// ReadCaretText читает текст перед курсором ввода в окне с фокусом.
// ok=false — программа не поддерживает чтение, текст выделен, это поле пароля или не успели за timeout.
func ReadCaretText(timeout time.Duration) (CaretText, bool) {
	select {
	case uiaStart <- struct{}{}:
		go uiaWorker()
	default:
	}
	req := uiaReq{reply: make(chan CaretText, 1), ok: make(chan bool, 1)}
	select {
	case uiaQueue <- req:
	default:
		return CaretText{}, false // предыдущий запрос ещё не завершён (программа не отвечает)
	}
	select {
	case ok := <-req.ok:
		if !ok {
			return CaretText{}, false
		}
		return <-req.reply, true
	case <-time.After(timeout):
		return CaretText{}, false
	}
}

func uiaWorker() {
	runtime.LockOSThread()
	pCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED — так советует Microsoft для клиентов UI Automation
	var uia uintptr
	if r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, 1|4, /*INPROC|LOCAL*/
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&uia))); r != 0 || uia == 0 {
		uia = 0
	}
	for req := range uiaQueue {
		var res CaretText
		ok := false
		if uia != 0 {
			Protect("чтение текста у курсора", func() { res, ok = uiaRead(uia) })
		}
		req.reply <- res
		req.ok <- ok
	}
}

func release(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

func bstrString(b *uint16) string {
	if b == nil {
		return ""
	}
	p := uintptr(unsafe.Pointer(b))
	defer pSysFreeString.Call(p)
	n, _, _ := pSysStringLen.Call(p)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice(b, n))
}

func uiaRead(uia uintptr) (CaretText, bool) {
	const (
		uiaTextPatternID   = 10014
		uiaIsPasswordProp  = 30019
		endpointStart      = 0
		endpointEnd        = 1
		textUnitCharacter  = 0
		vtBool             = 11
		iuiaGetFocused     = 8  // IUIAutomation::GetFocusedElement
		elemGetPropValue   = 10 // IUIAutomationElement::GetCurrentPropertyValue
		elemGetPattern     = 16 // IUIAutomationElement::GetCurrentPattern
		tpGetSelection     = 5  // IUIAutomationTextPattern::GetSelection
		arrLength          = 3  // IUIAutomationTextRangeArray::get_Length
		arrElement         = 4  // IUIAutomationTextRangeArray::GetElement
		rngClone           = 3  // IUIAutomationTextRange::Clone
		rngCompareEndpoint = 5
		rngGetText         = 12
		rngMoveEndpoint    = 14 // MoveEndpointByUnit
	)
	var el uintptr
	if r := comCall(uia, iuiaGetFocused, uintptr(unsafe.Pointer(&el))); r != 0 || el == 0 {
		return CaretText{}, false
	}
	defer release(el)

	var v [3]uint64 // VARIANT
	if r := comCall(el, elemGetPropValue, uiaIsPasswordProp, uintptr(unsafe.Pointer(&v[0]))); r == 0 &&
		uint16(v[0]) == vtBool && int16(v[1]) != 0 {
		return CaretText{}, false
	}

	var unk uintptr
	if r := comCall(el, elemGetPattern, uiaTextPatternID, uintptr(unsafe.Pointer(&unk))); r != 0 || unk == 0 {
		return CaretText{}, false
	}
	var tp uintptr
	r := comCall(unk, 0, uintptr(unsafe.Pointer(&iidTextPattern)), uintptr(unsafe.Pointer(&tp)))
	release(unk)
	if r != 0 || tp == 0 {
		return CaretText{}, false
	}
	defer release(tp)

	var arr uintptr
	if r := comCall(tp, tpGetSelection, uintptr(unsafe.Pointer(&arr))); r != 0 || arr == 0 {
		return CaretText{}, false
	}
	defer release(arr)
	var n int32
	if r := comCall(arr, arrLength, uintptr(unsafe.Pointer(&n))); r != 0 || n < 1 {
		return CaretText{}, false
	}
	var rng uintptr
	if r := comCall(arr, arrElement, 0, uintptr(unsafe.Pointer(&rng))); r != 0 || rng == 0 {
		return CaretText{}, false
	}
	defer release(rng)
	// Выделен текст — это не наш случай (выделение конвертируется отдельно).
	var cmp int32
	if r := comCall(rng, rngCompareEndpoint, endpointStart, rng, endpointEnd, uintptr(unsafe.Pointer(&cmp))); r != 0 || cmp != 0 {
		return CaretText{}, false
	}

	text := func(endpoint uintptr, count int32, max int) (string, bool) {
		var c uintptr
		if r := comCall(rng, rngClone, uintptr(unsafe.Pointer(&c))); r != 0 || c == 0 {
			return "", false
		}
		defer release(c)
		var moved int32
		if r := comCall(c, rngMoveEndpoint, endpoint, textUnitCharacter, uintptr(count), uintptr(unsafe.Pointer(&moved))); r != 0 {
			return "", false
		}
		var b *uint16
		if r := comCall(c, rngGetText, uintptr(max), uintptr(unsafe.Pointer(&b))); r != 0 {
			return "", false
		}
		return bstrString(b), true
	}
	before, ok := text(endpointStart, -80, 400)
	if !ok {
		return CaretText{}, false
	}
	after, _ := text(endpointEnd, 1, 8)
	res := CaretText{Before: before}
	for _, r := range after {
		res.AfterWord = unicode.IsLetter(r) || unicode.IsDigit(r)
		break
	}
	return res, true
}
