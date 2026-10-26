package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

var (
	colorActive = color.NRGBA{0x34, 0xd3, 0x99, 0xff} // emerald: tracking
	colorPaused = color.NRGBA{0x71, 0x71, 0x7a, 0xff} // zinc: paused
)

// faviconICO renders the "focus target" (ring + dot) as a multi-size
// .ico. Windows' tray uses the same bytes; Linux takes a PNG instead
// (see trayIcon in tray_linux.go).
func faviconICO(c color.NRGBA) []byte {
	sizes := []int{16, 24, 32, 48}
	pngs := make([][]byte, len(sizes))
	for i, s := range sizes {
		pngs[i] = renderIcon(s, c)
	}
	return encodeICO(sizes, pngs)
}

// renderIcon draws the icon at one size and returns PNG bytes.
func renderIcon(size int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	center := float64(size) / 2
	outer := center - 0.5
	ring := float64(size) * 0.16
	dot := float64(size) * 0.2
	const ss = 4 // 4x4 supersampling for smooth edges
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			hits := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					d := math.Hypot(px-center, py-center)
					if (d <= outer && d >= outer-ring) || d <= dot {
						hits++
					}
				}
			}
			if hits > 0 {
				a := uint8(int(c.A) * hits / (ss * ss))
				img.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, a})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// encodeICO wraps PNG images in an ICO container (PNG-compressed
// icons are supported since Windows Vista).
func encodeICO(sizes []int, pngs [][]byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&b, le, uint16(0))          // reserved
	binary.Write(&b, le, uint16(1))          // type: icon
	binary.Write(&b, le, uint16(len(sizes))) // image count
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := byte(s)
		if s >= 256 {
			dim = 0
		}
		b.Write([]byte{dim, dim, 0, 0})
		binary.Write(&b, le, uint16(1))  // color planes
		binary.Write(&b, le, uint16(32)) // bits per pixel
		binary.Write(&b, le, uint32(len(pngs[i])))
		binary.Write(&b, le, uint32(offset))
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes()
}
