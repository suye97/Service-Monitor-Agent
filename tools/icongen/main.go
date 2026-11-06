// Command icongen renders the Service Pulse application icon without external assets.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

type point struct{ x, y float64 }

func mix(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t), uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t), uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t), 255}
}
func blend(dst color.RGBA, src color.RGBA, alpha float64) color.RGBA {
	return color.RGBA{uint8(float64(dst.R) + (float64(src.R)-float64(dst.R))*alpha), uint8(float64(dst.G) + (float64(src.G)-float64(dst.G))*alpha), uint8(float64(dst.B) + (float64(src.B)-float64(dst.B))*alpha), 255}
}
func distance(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := ((p.x-a.x)*dx + (p.y-a.y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.x-a.x-t*dx, p.y-a.y-t*dy)
}
func rounded(x, y, left, top, right, bottom, radius float64) bool {
	if x < left || x >= right || y < top || y >= bottom {
		return false
	}
	cx, cy := x, y
	if x < left+radius { cx = left + radius } else if x >= right-radius { cx = right - radius - 1 }
	if y < top+radius { cy = top + radius } else if y >= bottom-radius { cy = bottom - radius - 1 }
	return (cx == x && cy == y) || math.Hypot(x-cx, y-cy) < radius
}

func render(size int) image.Image {
	const high = 1024
	scale := float64(high) / 256
	img := image.NewRGBA(image.Rect(0, 0, high, high))
	line := []point{{55, 130}, {88, 130}, {108, 82}, {136, 174}, {158, 116}, {201, 130}}
	for y := 0; y < high; y++ {
		for x := 0; x < high; x++ {
			px, py := float64(x)/scale, float64(y)/scale
			if !rounded(px, py, 20, 20, 236, 236, 54) {
				continue
			}
			base := mix(color.RGBA{27, 49, 80, 255}, color.RGBA{12, 20, 39, 255}, math.Min(1, (px+py)/470))
			img.SetRGBA(x, y, base)
			nearest := 1000.0
			for i := 1; i < len(line); i++ {
				nearest = math.Min(nearest, distance(point{px, py}, line[i-1], line[i]))
			}
			pulse := mix(color.RGBA{104, 238, 228, 255}, color.RGBA{120, 146, 255, 255}, math.Max(0, math.Min(1, (px-55)/146)))
			if nearest < 14 {
				img.SetRGBA(x, y, blend(img.RGBAAt(x, y), pulse, (14-nearest)/14*.16))
			}
			if nearest < 6 {
				img.SetRGBA(x, y, pulse)
			}
		}
	}
	if size == high {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(out, out.Bounds(), img, img.Bounds(), draw.Over, nil)
	return out
}

func main() {
	out := flag.String("out", "build", "asset output directory")
	flag.Parse()
	if err := os.MkdirAll(filepath.Join(*out, "windows"), 0755); err != nil {
		panic(err)
	}
	pngData, err := os.Create(filepath.Join(*out, "appicon.png"))
	if err != nil { panic(err) }
	if err := png.Encode(pngData, render(256)); err != nil { panic(err) }
	if err := pngData.Close(); err != nil { panic(err) }

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var images [][]byte
	for _, size := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, render(size)); err != nil { panic(err) }
		images = append(images, buf.Bytes())
	}
	var icon bytes.Buffer
	_ = binary.Write(&icon, binary.LittleEndian, uint16(0))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(len(images)))
	offset := 6 + 16*len(images)
	for i, data := range images {
		width, height := byte(sizes[i]), byte(sizes[i])
		if sizes[i] == 256 { width, height = 0, 0 }
		icon.Write([]byte{width, height, 0, 0})
		_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
		_ = binary.Write(&icon, binary.LittleEndian, uint16(32))
		_ = binary.Write(&icon, binary.LittleEndian, uint32(len(data)))
		_ = binary.Write(&icon, binary.LittleEndian, uint32(offset))
		offset += len(data)
	}
	for _, data := range images { icon.Write(data) }
	if err := os.WriteFile(filepath.Join(*out, "windows", "icon.ico"), icon.Bytes(), 0644); err != nil { panic(err) }
}
