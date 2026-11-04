// genicon draws the application icon (a 4x2 grid of camera tiles).
// Run: go run ./tools/genicon
// It writes PNG files for the tray icon and an .ico for the exe.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

const ss = 4 // supersampling

func draw256(size int) *image.RGBA {
	n := size * ss
	big := image.NewRGBA(image.Rect(0, 0, n, n))
	f := float64(n) / 100
	fill := func(x0, y0, x1, y1, r float64, c color.RGBA) {
		for y := int(y0 * f); y < int(y1*f); y++ {
			for x := int(x0 * f); x < int(x1*f); x++ {
				if inRound(float64(x)+.5, float64(y)+.5, x0*f, y0*f, x1*f, y1*f, r*f) {
					big.SetRGBA(x, y, c)
				}
			}
		}
	}
	fill(2, 2, 98, 98, 16, color.RGBA{0x1b, 0x2a, 0x41, 0xff}) // background
	tile := color.RGBA{0x4f, 0xa3, 0xe0, 0xff}
	live := color.RGBA{0x3c, 0xc0, 0x6a, 0xff}
	const m, g = 11.0, 5.0
	w := (100 - 2*m - 3*g) / 4
	h := (100 - 2*m - g) / 2
	for r := 0; r < 2; r++ {
		for c := 0; c < 4; c++ {
			x := m + float64(c)*(w+g)
			y := m + float64(r)*(h+g)
			col := tile
			if r == 0 && c == 0 {
				col = live
			}
			fill(x, y, x+w, y+h, 4, col)
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, cnt int
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					p := big.RGBAAt(x*ss+dx, y*ss+dy)
					r += int(p.R) * int(p.A)
					g += int(p.G) * int(p.A)
					b += int(p.B) * int(p.A)
					a += int(p.A)
					cnt++
				}
			}
			if a > 0 {
				out.SetRGBA(x, y, color.RGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / cnt)})
			}
		}
	}
	return out
}

func inRound(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px >= x1 || py < y0 || py >= y1 {
		return false
	}
	cx, cy := px, py
	switch {
	case px < x0+r:
		cx = x0 + r
	case px > x1-r:
		cx = x1 - r
	}
	switch {
	case py < y0+r:
		cy = y0 + r
	case py > y1-r:
		cy = y1 - r
	}
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func main() {
	_ = draw.Src
	sizes := []int{16, 24, 32, 48, 64, 256}
	var datas [][]byte
	for _, s := range sizes {
		d := pngBytes(draw256(s))
		datas = append(datas, d)
		if s <= 48 {
			os.WriteFile("internal/window/assets/icon"+itoa(s)+".png", d, 0o644)
		}
	}
	// ICO file with PNG images
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for i, s := range sizes {
		wh := byte(s)
		if s >= 256 {
			wh = 0
		}
		ico.Write([]byte{wh, wh, 0, 0})
		binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&ico, binary.LittleEndian, uint32(len(datas[i])))
		binary.Write(&ico, binary.LittleEndian, uint32(off))
		off += len(datas[i])
	}
	for _, d := range datas {
		ico.Write(d)
	}
	os.WriteFile("assets/app.ico", ico.Bytes(), 0o644)
	os.WriteFile("assets/icon.png", datas[len(datas)-1], 0o644)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}
