//go:build ignore

// gen-app-icon writes cmd/tray/AppIcon.icns through macOS iconutil(1) and
// cmd/tray/AppIcon_preview.png. The artwork uses no Proton trademark or logo.
//
// Usage: go run scripts/gen-app-icon.go
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	colorTop    = color.NRGBA{R: 109, G: 74, B: 255, A: 255} // #6D4AFF
	colorBottom = color.NRGBA{R: 80, G: 50, B: 200, A: 255}  // #5032C8
	colorWhite  = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

type iconSpec struct {
	size  int
	scale int
	name  string
}

var specs = []iconSpec{
	{16, 1, "icon_16x16.png"},
	{16, 2, "icon_16x16@2x.png"},
	{32, 1, "icon_32x32.png"},
	{32, 2, "icon_32x32@2x.png"},
	{128, 1, "icon_128x128.png"},
	{128, 2, "icon_128x128@2x.png"},
	{256, 1, "icon_256x256.png"},
	{256, 2, "icon_256x256@2x.png"},
	{512, 1, "icon_512x512.png"},
	{512, 2, "icon_512x512@2x.png"},
}

func main() {
	iconsetDir := filepath.Join("cmd", "tray", "AppIcon.iconset")
	_ = os.MkdirAll(iconsetDir, 0o755)

	for _, spec := range specs {
		px := spec.size * spec.scale
		img := renderIcon(px)
		writePNG(filepath.Join(iconsetDir, spec.name), img)
		fmt.Printf("  %s (%dx%d)\n", spec.name, px, px)
	}

	preview := renderIcon(512)
	previewPath := filepath.Join("cmd", "tray", "AppIcon_preview.png")
	writePNG(previewPath, preview)
	fmt.Printf("  Preview: %s\n", previewPath)

	icnsPath := filepath.Join("cmd", "tray", "AppIcon.icns")
	cmd := exec.Command("iconutil", "-c", "icns", iconsetDir, "-o", icnsPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "iconutil failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "The .iconset directory has been created — run iconutil manually if needed.")
		os.Exit(1)
	}

	_ = os.RemoveAll(iconsetDir)
	fmt.Printf("Generated %s\n", icnsPath)
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		panic(err)
	}
	_ = f.Close()
}

func renderIcon(sz int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	s := float64(sz)
	radius := s * 0.18

	for y := 0; y < sz; y++ {
		t := float64(y) / s
		bg := lerpColor(colorTop, colorBottom, t)
		for x := 0; x < sz; x++ {
			if insideRoundedRect(float64(x)+0.5, float64(y)+0.5, s, s, radius) {
				img.SetNRGBA(x, y, bg)
			}
		}
	}

	drawTransferArrows(img, sz)

	return img
}

func drawTransferArrows(img *image.NRGBA, sz int) {
	s := float64(sz)

	shaftW := s * 0.10
	headW := s * 0.28
	headH := s * 0.16
	gap := s * 0.18 // from the center to each arrow's axis
	topY := s * 0.22
	botY := s * 0.78
	overlap := s * 0.02

	lcx := s/2 - gap
	fillRectSolid(img, lcx-shaftW/2, topY+headH-overlap, lcx+shaftW/2, botY, colorWhite)
	fillTriangleUp(img, lcx, topY, headW, headH, colorWhite)

	rcx := s/2 + gap
	fillRectSolid(img, rcx-shaftW/2, topY, rcx+shaftW/2, botY-headH+overlap, colorWhite)
	fillTriangleDown(img, rcx, botY, headW, headH, colorWhite)
}

// fillRectSolid fills an axis-aligned rectangle with solid color.
func fillRectSolid(img *image.NRGBA, x0, y0, x1, y1 float64, col color.NRGBA) {
	bounds := img.Bounds()
	ix0 := int(math.Ceil(x0))
	iy0 := int(math.Ceil(y0))
	ix1 := int(math.Floor(x1))
	iy1 := int(math.Floor(y1))
	for y := iy0; y <= iy1 && y < bounds.Max.Y; y++ {
		for x := ix0; x <= ix1 && x < bounds.Max.X; x++ {
			if x >= 0 && y >= 0 {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

// fillTriangleUp draws a filled upward-pointing triangle with tip at (cx, tipY).
func fillTriangleUp(img *image.NRGBA, cx, tipY, w, h float64, col color.NRGBA) {
	bounds := img.Bounds()
	baseY := tipY + h
	for y := int(tipY); y <= int(baseY) && y < bounds.Max.Y; y++ {
		if y < 0 {
			continue
		}
		fy := float64(y) + 0.5
		if fy < tipY || fy > baseY {
			continue
		}
		t := (fy - tipY) / h
		halfW := t * w / 2
		left := int(math.Ceil(cx - halfW))
		right := int(math.Floor(cx + halfW))
		for x := left; x <= right && x < bounds.Max.X; x++ {
			if x >= 0 {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

// fillTriangleDown draws a filled downward-pointing triangle with tip at (cx, tipY).
func fillTriangleDown(img *image.NRGBA, cx, tipY, w, h float64, col color.NRGBA) {
	bounds := img.Bounds()
	topY := tipY - h
	for y := int(topY); y <= int(tipY) && y < bounds.Max.Y; y++ {
		if y < 0 {
			continue
		}
		fy := float64(y) + 0.5
		if fy < topY || fy > tipY {
			continue
		}
		t := (tipY - fy) / h
		halfW := t * w / 2
		left := int(math.Ceil(cx - halfW))
		right := int(math.Floor(cx + halfW))
		for x := left; x <= right && x < bounds.Max.X; x++ {
			if x >= 0 {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

// insideRoundedRect checks if point (px,py) is inside a rounded rectangle
// spanning [0,0] to [w,h] with corner radius r.
func insideRoundedRect(px, py, w, h, r float64) bool {
	if px < 0 || px > w || py < 0 || py > h {
		return false
	}
	if px >= r && px <= w-r {
		return true
	}
	if py >= r && py <= h-r {
		return true
	}
	var cx, cy float64
	if px < r {
		cx = r
	} else {
		cx = w - r
	}
	if py < r {
		cy = r
	} else {
		cy = h - r
	}
	dx := px - cx
	dy := py - cy
	return dx*dx+dy*dy <= r*r
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: uint8(float64(a.A)*(1-t) + float64(b.A)*t),
	}
}
