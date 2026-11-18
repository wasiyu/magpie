//go:build !nogui

package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	xdraw "golang.org/x/image/draw"
)

var jellytoolTrayOnce struct {
	sync.Once
	icon []byte
}

// jellytoolTrayGlyph turns Jellytoken's coloured brand mark into the same
// 44px alpha glyph used by the ordinary menu bar icon. The 4px transparent
// inset leaves an 18pt mark in macOS's 22pt status item, matching neighbouring
// status icons while keeping Jellytoken's rounded shape recognisable.
func jellytoolTrayGlyph() []byte {
	jellytoolTrayOnce.Do(func() {
		src, err := png.Decode(bytes.NewReader(jellytoolIcon()))
		if err != nil {
			jellytoolTrayOnce.icon = jellytoolIcon()
			return
		}
		alpha := image.NewNRGBA(src.Bounds())
		for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
			for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
				p := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
				a := p.A
				// Keep the face as negative space in the filled glyph. The
				// dark border is outside this inset; only the eyes and smile
				// are removed, so the mark stays recognisable at 18pt.
				if x >= 48 && x < 208 && y >= 48 && y < 208 &&
					int(p.R)+int(p.G)+int(p.B) < 510 {
					a = 0
				}
				alpha.SetNRGBA(x, y, color.NRGBA{255, 255, 255, a})
			}
		}
		out := image.NewNRGBA(image.Rect(0, 0, 44, 44))
		xdraw.CatmullRom.Scale(out, image.Rect(4, 4, 40, 40), alpha, alpha.Bounds(), draw.Src, nil)
		clearJellytoolFace(out)
		var buf bytes.Buffer
		if err := png.Encode(&buf, out); err != nil {
			jellytoolTrayOnce.icon = jellytoolIcon()
			return
		}
		jellytoolTrayOnce.icon = buf.Bytes()
	})
	return jellytoolTrayOnce.icon
}

// clearJellytoolFace enlarges the logo's negative-space details for the
// status bar. The source artwork's face is legible at app-icon sizes, but its
// original holes become sub-pixel after NSStatusItem scales the glyph to 18pt.
func clearJellytoolFace(img *image.NRGBA) {
	clearEllipse := func(cx, cy, rx, ry float64) {
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				dx, dy := (float64(x)+.5-cx)/rx, (float64(y)+.5-cy)/ry
				if dx*dx+dy*dy <= 1 {
					p := img.NRGBAAt(x, y)
					p.A = 0
					img.SetNRGBA(x, y, p)
				}
			}
		}
	}
	clearEllipse(17, 20, 2.4, 4.2)
	clearEllipse(27, 20, 2.4, 4.2)
	// A broad, shallow smile: the centre sits lower than its ends.
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if x < 15 || x > 30 {
				continue
			}
			d := (float64(x) + .5 - 22) / 7
			curve := 29.5 - 3.0*d*d
			if jellytoolAbs(float64(y)+.5-curve) <= 1.35 {
				p := img.NRGBAAt(x, y)
				p.A = 0
				img.SetNRGBA(x, y, p)
			}
		}
	}
}

func jellytoolAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// jellytoolTrayIconFor follows the ordinary tray's platform policy: template
// alpha on macOS, a theme-coloured glyph on Linux, and the full colour brand
// icon on Windows where tray icons are application-owned.
func jellytoolTrayIconFor(goos string, light bool) (icon []byte, template bool) {
	switch goos {
	case "darwin":
		return jellytoolTrayGlyph(), true
	case "linux":
		if light {
			return trayGlyphFrom(jellytoolTrayGlyph(), trayGlyphDark), false
		}
		return trayGlyphFrom(jellytoolTrayGlyph(), trayGlyphLight), false
	default:
		return jellytoolIcon(), false
	}
}
