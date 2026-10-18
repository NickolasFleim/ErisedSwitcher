// Package trayicon рисует значок для трея (две буквы на цветной плашке) в пиксели BGRA.
package trayicon

// 5×7 пиксельный шрифт для нужных букв.
var glyphs = map[rune][7]string{
	'E': {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'?': {".###.", "#...#", "....#", "..##.", "..#..", ".....", "..#.."},
	'-': {".....", ".....", ".....", "#####", ".....", ".....", "....."},
	'O': {".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'F': {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'S': {".####", "#....", "#....", ".###.", "....#", "....#", "####."},
	'М': {"#...#", "##.##", "#.#.#", "#...#", "#...#", "#...#", "#...#"},
}

// Color — цвет RGB.
type Color struct{ R, G, B uint8 }

// Цвета плашек.
var (
	BlueEN  = Color{0x25, 0x63, 0xEB}
	RedRU   = Color{0xDC, 0x26, 0x26}
	GrayOff = Color{0x6B, 0x72, 0x80}
	Amber   = Color{0xD9, 0x77, 0x06}
	White   = Color{0xFF, 0xFF, 0xFF}
)

// Render возвращает size×size пикселей BGRA сверху вниз.
func Render(size int, text string, bg, fg Color, crossed bool) []byte {
	px := make([]byte, size*size*4)
	set := func(x, y int, c Color, a uint8) {
		if x < 0 || y < 0 || x >= size || y >= size {
			return
		}
		i := (y*size + x) * 4
		// Premultiplied alpha для 32-битных значков Windows.
		px[i] = uint8(int(c.B) * int(a) / 255)
		px[i+1] = uint8(int(c.G) * int(a) / 255)
		px[i+2] = uint8(int(c.R) * int(a) / 255)
		px[i+3] = a
	}
	// Плашка со скруглёнными углами.
	r := size / 6
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if inRounded(x, y, size, r) {
				set(x, y, bg, 255)
			}
		}
	}
	scale := size / 12
	if scale < 1 {
		scale = 1
	}
	rs := []rune(text)
	gw, gh, gap := 5*scale, 7*scale, scale
	total := len(rs)*gw + (len(rs)-1)*gap
	x0 := (size - total) / 2
	y0 := (size - gh) / 2
	for i, ch := range rs {
		g, ok := glyphs[ch]
		if !ok {
			g = glyphs['?']
		}
		for gy := 0; gy < 7; gy++ {
			for gx := 0; gx < 5; gx++ {
				if g[gy][gx] != '#' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						set(x0+i*(gw+gap)+gx*scale+dx, y0+gy*scale+dy, fg, 255)
					}
				}
			}
		}
	}
	if crossed {
		for t := 0; t < size; t++ {
			for w := 0; w < scale; w++ {
				set(t, size-1-t+w, fg, 255)
			}
		}
	}
	return px
}

func inRounded(x, y, size, r int) bool {
	cx, cy := x, y
	switch {
	case x < r && y < r:
		cx, cy = r, r
	case x >= size-r && y < r:
		cx, cy = size-r-1, r
	case x < r && y >= size-r:
		cx, cy = r, size-r-1
	case x >= size-r && y >= size-r:
		cx, cy = size-r-1, size-r-1
	default:
		return true
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}
