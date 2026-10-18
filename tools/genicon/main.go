// genicon рисует assets/erised.ico (значок exe) и assets/erised.png (предпросмотр).
//
//	go run ./tools/genicon
//	cd cmd/erised && go-winres make --arch amd64,arm64   (go install github.com/tc-hib/go-winres@v0.3.3)
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"

	"erised/internal/trayicon"
)

func pngOf(size int) []byte {
	bgra := trayicon.RenderMirror(size, true)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < size*size; i++ {
		img.Pix[i*4+0] = bgra[i*4+2]
		img.Pix[i*4+1] = bgra[i*4+1]
		img.Pix[i*4+2] = bgra[i*4+0]
		img.Pix[i*4+3] = bgra[i*4+3]
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func main() {
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 256}
	var imgs [][]byte
	for _, s := range sizes {
		imgs = append(imgs, pngOf(s))
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for i, s := range sizes {
		d := uint8(s)
		if s >= 256 {
			d = 0
		}
		binary.Write(&out, binary.LittleEndian, struct {
			W, H, Colors, Res uint8
			Planes, Bpp       uint16
			Size, Offset      uint32
		}{d, d, 0, 0, 1, 32, uint32(len(imgs[i])), uint32(off)})
		off += len(imgs[i])
	}
	for _, b := range imgs {
		out.Write(b)
	}
	must(os.WriteFile("assets/erised.ico", out.Bytes(), 0o644))
	must(os.WriteFile("cmd/erised/winres/icon.ico", out.Bytes(), 0o644))
	must(os.WriteFile("assets/erised.png", pngOf(256), 0o644))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
