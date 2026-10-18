package trayicon

import "math"

// RenderMirror рисует значок программы — арочное зеркало в золотой раме — размером
// size×size в пикселях BGRA с premultiplied alpha (как Render).
// straight=true — обычная (не premultiplied) альфа, для PNG внутри .ico.
func RenderMirror(size int, straight bool) []byte {
	px := make([]byte, size*size*4)
	const ss = 4 // сглаживание: 4×4 выборки на пиксель
	s := float64(size)
	// Геометрия в долях размера.
	cx := s / 2
	outerW := s * 0.70
	outerTop := s * 0.04
	outerBottom := s * 0.86
	frame := math.Max(1.2, s*0.085)
	r := outerW / 2

	inArch := func(x, y, half, top, bottom float64) bool {
		if y > bottom || x < cx-half || x > cx+half {
			return false
		}
		ay := top + half // центр полукруга
		if y >= ay {
			return y >= top
		}
		dx, dy := x-cx, y-ay
		return dx*dx+dy*dy <= half*half
	}
	gold := [3]float64{0xC9, 0x9A, 0x3B}
	goldDark := [3]float64{0x8A, 0x63, 0x1E}
	for py := 0; py < size; py++ {
		for pxl := 0; pxl < size; pxl++ {
			var acc [4]float64 // r g b a (premultiplied)
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := float64(pxl) + (float64(sx)+0.5)/ss
					y := float64(py) + (float64(sy)+0.5)/ss
					var c [3]float64
					hit := false
					switch {
					case inArch(x, y, r-frame, outerTop+frame, outerBottom-frame):
						// Стекло: глубокий синий сверху, светлее книзу, с косым бликом.
						t := (y - outerTop) / (outerBottom - outerTop)
						c = [3]float64{0x1B + 40*t, 0x2A + 60*t, 0x4F + 80*t}
						u := (x - cx) + (y - s*0.45)
						if u > -s*0.20 && u < -s*0.12 {
							c = [3]float64{0xD6, 0xE4, 0xF5}
						}
						hit = true
					case inArch(x, y, r, outerTop, outerBottom):
						// Рама: свет слева сверху.
						k := 0.5 + 0.5*((cx-x)/r*0.6+(s*0.5-y)/s*0.8)
						k = math.Max(0, math.Min(1, k))
						for i := range c {
							c[i] = goldDark[i] + (gold[i]-goldDark[i])*k
						}
						hit = true
					case y > outerBottom && y <= s*0.97 &&
						((x > cx-r*0.95 && x < cx-r*0.55) || (x > cx+r*0.55 && x < cx+r*0.95)):
						c = goldDark // ножки
						hit = true
					}
					if hit {
						acc[0] += c[0]
						acc[1] += c[1]
						acc[2] += c[2]
						acc[3] += 255
					}
				}
			}
			n := float64(ss * ss)
			a := acc[3] / n
			i := (py*size + pxl) * 4
			if a == 0 {
				continue
			}
			// acc содержит сумму цветов только по попавшим выборкам: это premultiplied.
			rr, gg, bb := acc[0]/n, acc[1]/n, acc[2]/n
			if straight {
				f := 255 / a
				rr, gg, bb = rr*f, gg*f, bb*f
			}
			px[i] = uint8(math.Min(255, bb))
			px[i+1] = uint8(math.Min(255, gg))
			px[i+2] = uint8(math.Min(255, rr))
			px[i+3] = uint8(math.Min(255, a))
		}
	}
	return px
}
