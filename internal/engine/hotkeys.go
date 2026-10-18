package engine

import (
	"erised/internal/config"
	"erised/internal/keymap"
)

type combo struct {
	hk  config.Hotkey
	act HotkeyAction
}

type hotkeys struct {
	combos []combo
	double map[config.TapKey]HotkeyAction
	single map[config.TapKey]HotkeyAction
}

func parseHotkeys(c *config.Config) hotkeys {
	h := hotkeys{double: map[config.TapKey]HotkeyAction{}, single: map[config.TapKey]HotkeyAction{}}
	for _, it := range []struct {
		s string
		a HotkeyAction
	}{
		{c.Hotkeys.ConvertWord, HKConvertWord},
		{c.Hotkeys.ConvertPhrase, HKConvertPhrase},
		{c.Hotkeys.ConvertSelection, HKConvertSelection},
		{c.Hotkeys.Undo, HKUndo},
		{c.Hotkeys.ToggleEnabled, HKToggleEnabled},
		{c.Hotkeys.SwitchLayout, HKSwitchLayout},
		{c.Hotkeys.ToggleApp, HKToggleApp},
	} {
		hk, err := config.ParseHotkey(it.s)
		if err != nil || hk.Empty {
			continue
		}
		switch {
		case hk.Double:
			if _, dup := h.double[hk.Tap]; !dup {
				h.double[hk.Tap] = it.a
			}
		case hk.VK == 0:
			if _, dup := h.single[hk.Tap]; !dup {
				h.single[hk.Tap] = it.a
			}
		default:
			h.combos = append(h.combos, combo{hk, it.a})
		}
	}
	return h
}

func (h *hotkeys) match(ev KeyEvent) (HotkeyAction, bool) {
	vk := ev.VK
	if vk == 0x03 { // VK_CANCEL = Ctrl+Pause
		vk = keymap.VK_PAUSE
	}
	m := ev.Mods
	for _, c := range h.combos {
		if c.hk.VK == vk && c.hk.Ctrl == m.Ctrl() && c.hk.Alt == m.Alt() && c.hk.Shift == m.Shift() && c.hk.Win == m.Win() {
			return c.act, true
		}
	}
	return 0, false
}
