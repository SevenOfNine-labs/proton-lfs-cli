//go:build ignore

// gen-app-icon generates a macOS .icns app icon for Proton LFS.
//
// Design: Rounded-rectangle background in Proton purple (#6D4AFF) with a
// subtle gradient, white paired vertical arrows (upload ↑ / download ↓)
// representing file transfer. The design is original and does not use any
// Proton trademarks or logos.
//
// Usage: go run scripts/gen-app-icon.go
//
// Requires macOS `iconutil` to produce the final .icns file.
// Also writes a preview PNG to cmd/tray/AppIcon_preview.png.
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

// Proton-inspired palette (no trademarked logo elements).
var (
	colorTop    = color.NRGBA{R: 109, G: 74, B: 255, A: 255} // #6D4AFF
	colorBottom = color.NRGBA{R: 80, G: 50, B: 200, A: 255}  // deeper violet
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

	// Write a 512px preview PNG for easy visual check
	preview := renderIcon(512)
	previewPath := filepath.Join("cmd", "tray", "AppIcon_preview.png")
	writePNG(previewPath, preview)
	fmt.Printf("  Preview: %s\n", previewPath)

	// Run iconutil to produce .icns
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

	// 1. Draw rounded-rect background with vertical gradient.
	for y := 0; y < sz; y++ {
		t := float64(y) / s
		bg := lerpColor(colorTop, colorBottom, t)
		for x := 0; x < sz; x++ {
			if insideRoundedRect(float64(x)+0.5, float64(y)+0.5, s, s, radius) {
				img.SetNRGBA(x, y, bg)
			}
		}
	}

	// 2. Draw white transfer arrows.
	drawTransferArrows(img, sz)

	return img
}

func drawTransferArrows(img *image.NRGBA, sz int) {
	s := float64(sz)

	shaftW := s * 0.10  // shaft width
	headW := s * 0.28   // arrowhead total width
	headH := s * 0.16   // arrowhead height
	gap := s * 0.18     // offset from center to each arrow center
	topY := s * 0.22    // top extent
	botY := s * 0.78    // bottom extent
	overlap := s * 0.02 // shaft overlaps into arrowhead slightly

	// Left arrow: pointing UP
	lcx := s/2 - gap
	fillRectSolid(img, lcx-shaftW/2, topY+headH-overlap, lcx+shaftW/2, botY, colorWhite)
	fillTriangleUp(img, lcx, topY, headW, headH, colorWhite)

	// Right arrow: pointing DOWN
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
	// If not near a corner, definitely inside
	if px >= r && px <= w-r {
		return true
	}
	if py >= r && py <= h-r {
		return true
	}
	// In a corner zone — find the nearest corner center
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
