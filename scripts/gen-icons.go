//go:build ignore

// gen-icons generates 64x64 PNG tray icons for Proton Git LFS.
// Each icon shows a pair of vertical arrows (upload ↑ download ↓) forming
// a cycle motif, rendered in a state-specific color on a transparent background.
//
// Usage: go run scripts/gen-icons.go
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

const size = 64

type iconDef struct {
	name string
	col  color.NRGBA
}

var icons = []iconDef{
	{"icon_idle.png", color.NRGBA{R: 136, G: 136, B: 136, A: 255}},   // grey
	{"icon_ok.png", color.NRGBA{R: 76, G: 175, B: 80, A: 255}},       // green
	{"icon_error.png", color.NRGBA{R: 244, G: 67, B: 54, A: 255}},    // red
	{"icon_syncing.png", color.NRGBA{R: 33, G: 150, B: 243, A: 255}}, // blue
}

func main() {
	dir := filepath.Join("cmd", "tray", "icons")
	_ = os.MkdirAll(dir, 0o755)

	for _, ic := range icons {
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		drawArrows(img, ic.col)
		f, err := os.Create(filepath.Join(dir, ic.name))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, img); err != nil {
			_ = f.Close()
			panic(err)
		}
		_ = f.Close()
	}
}

// drawArrows renders two arrows: up-arrow on left half, down-arrow on right half.
func drawArrows(img *image.NRGBA, col color.NRGBA) {
	// Up arrow (left half): shaft from y=44 to y=20, head at y=16
	drawVLine(img, 20, 20, 44, col, 4)
	drawArrowHead(img, 20, 16, col, true) // pointing up

	// Down arrow (right half): shaft from y=20 to y=44, head at y=48
	drawVLine(img, 44, 20, 44, col, 4)
	drawArrowHead(img, 44, 48, col, false) // pointing down
}

// drawVLine draws a vertical line of given width centered on x.
func drawVLine(img *image.NRGBA, cx, y0, y1 int, col color.NRGBA, w int) {
	half := w / 2
	for y := y0; y <= y1; y++ {
		for x := cx - half; x < cx+half; x++ {
			if x >= 0 && x < size && y >= 0 && y < size {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

// drawArrowHead draws a triangular arrowhead. up=true points upward.
func drawArrowHead(img *image.NRGBA, cx, tipY int, col color.NRGBA, up bool) {
	headH := 10
	for row := 0; row < headH; row++ {
		var y int
		if up {
			y = tipY + row
		} else {
			y = tipY - row
		}
		halfW := row + 1
		for x := cx - halfW; x <= cx+halfW; x++ {
			if x >= 0 && x < size && y >= 0 && y < size {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}
