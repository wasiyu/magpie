//go:build !nogui

package gui

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

func TestJellytoolTrayIconPerOS(t *testing.T) {
	mac, template := jellytoolTrayIconFor("darwin", false)
	if !template {
		t.Fatal("macOS Jellytool tray icon must be a template image")
	}
	img, err := png.Decode(bytes.NewReader(mac))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got.X != 44 || got.Y != 44 {
		t.Fatalf("macOS Jellytool tray icon is %v, want 44x44", got)
	}
	shown := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if p.A > 0 {
				shown++
			}
		}
	}
	if shown == 0 || color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA).A != 0 ||
		color.NRGBAModel.Convert(img.At(43, 43)).(color.NRGBA).A != 0 {
		t.Fatalf("Jellytool tray glyph has %d visible pixels, want a padded shape", shown)
	}
	if color.NRGBAModel.Convert(img.At(17, 21)).(color.NRGBA).A != 0 {
		t.Fatal("Jellytool tray glyph lost the face's negative space")
	}
	if color.NRGBAModel.Convert(img.At(22, 30)).(color.NRGBA).A != 0 {
		t.Fatal("Jellytool tray glyph lost the smile's negative space")
	}

	for _, c := range []struct {
		light bool
		want  color.NRGBA
	}{{false, trayGlyphLight}, {true, trayGlyphDark}} {
		icon, template := jellytoolTrayIconFor("linux", c.light)
		if template {
			t.Errorf("Linux Jellytool tray icon is a template")
		}
		got, err := png.Decode(bytes.NewReader(icon))
		if err != nil {
			t.Fatal(err)
		}
		for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
			for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
				p := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
				if p.A > 0 && (p.R != c.want.R || p.G != c.want.G || p.B != c.want.B) {
					t.Fatalf("Linux Jellytool tray light=%v has coloured pixel %v", c.light, p)
				}
			}
		}
	}

	win, template := jellytoolTrayIconFor("windows", false)
	if template || !bytes.Equal(win, jellytoolIcon()) {
		t.Fatal("Windows Jellytool tray icon must keep the colour brand icon")
	}
}
