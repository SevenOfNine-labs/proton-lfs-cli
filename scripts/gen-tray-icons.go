//go:build ignore

// gen-tray-icons writes the 64x64 tray icons cmd/tray/icons/icon_{idle,ok,
// error,syncing}.png: the dark shape of a source image, tinted per state.
//
// Usage: go run scripts/gen-tray-icons.go <source-image>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	// Registers the GIF and JPEG decoders with image.Decode (Go image package).
	_ "image/gif"
	_ "image/jpeg"
)

const outSize = 64

type tintDef struct {
	name string
	col  color.NRGBA
}

var tints = []tintDef{
	{"icon_idle.png", color.NRGBA{R: 136, G: 136, B: 136, A: 255}},   // #888888
	{"icon_ok.png", color.NRGBA{R: 76, G: 175, B: 80, A: 255}},       // #4CAF50
	{"icon_error.png", color.NRGBA{R: 244, G: 67, B: 54, A: 255}},    // #F44336
	{"icon_syncing.png", color.NRGBA{R: 33, G: 150, B: 243, A: 255}}, // #2196F3
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/gen-tray-icons.go <source-image>")
		os.Exit(2)
	}

	src := loadImage(os.Args[1])
	mask := extractMask(src)

	outDir := filepath.Join("cmd", "tray", "icons")
	_ = os.MkdirAll(outDir, 0o755)

	for _, t := range tints {
		img := applyTint(mask, t.col)
		path := filepath.Join(outDir, t.name)
		writePNG(path, img)
		fmt.Printf("  %s\n", t.name)
	}
	fmt.Println("Done.")
}

func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open %s: %v", path, err))
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		panic(fmt.Sprintf("decode %s: %v", path, err))
	}
	return img
}

// extractMask box-filters the central 60% of src (inside the macOS icon frame)
// to an outSize alpha mask in which dark pixels are opaque.
func extractMask(src image.Image) *image.Alpha {
	bounds := src.Bounds()
	sw := bounds.Dx()
	sh := bounds.Dy()

	cropMargin := 0.20
	cropX0 := bounds.Min.X + int(float64(sw)*cropMargin)
	cropY0 := bounds.Min.Y + int(float64(sh)*cropMargin)
	cropX1 := bounds.Max.X - int(float64(sw)*cropMargin)
	cropY1 := bounds.Max.Y - int(float64(sh)*cropMargin)
	cw := cropX1 - cropX0
	ch := cropY1 - cropY0

	mask := image.NewAlpha(image.Rect(0, 0, outSize, outSize))

	for y := 0; y < outSize; y++ {
		for x := 0; x < outSize; x++ {
			srcX0 := cropX0 + x*cw/outSize
			srcY0 := cropY0 + y*ch/outSize
			srcX1 := cropX0 + (x+1)*cw/outSize
			srcY1 := cropY0 + (y+1)*ch/outSize

			var totalAlpha float64
			count := 0
			for sy := srcY0; sy < srcY1; sy++ {
				for sx := srcX0; sx < srcX1; sx++ {
					r, g, b, a := src.At(sx, sy).RGBA()
					if a == 0 {
						count++
						continue
					}
					rf := float64(r) / 65535.0
					gf := float64(g) / 65535.0
					bf := float64(b) / 65535.0
					// Luma weights of ITU-R BT.601.
					lum := 0.299*rf + 0.587*gf + 0.114*bf
					alpha := 1.0 - smoothstep(0.35, 0.55, lum)
					totalAlpha += alpha
					count++
				}
			}

			if count > 0 {
				avg := totalAlpha / float64(count)
				mask.SetAlpha(x, y, color.Alpha{A: uint8(avg * 255)})
			}
		}
	}

	return mask
}

// applyTint creates an NRGBA image by coloring the mask with the given tint.
func applyTint(mask *image.Alpha, tint color.NRGBA) *image.NRGBA {
	bounds := mask.Bounds()
	img := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			a := mask.AlphaAt(x, y).A
			if a > 0 {
				img.SetNRGBA(x, y, color.NRGBA{
					R: tint.R,
					G: tint.G,
					B: tint.B,
					A: a,
				})
			}
		}
	}
	return img
}

func smoothstep(edge0, edge1, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-edge0)/(edge1-edge0)))
	return t * t * (3 - 2*t)
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
