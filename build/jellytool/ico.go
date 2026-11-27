//go:build ignore

// ico writes Jellytool's app artwork as a Windows .ico, one PNG entry per
// size Windows asks for, so the exe, its shortcuts, the taskbar and the
// installer all show the Dock's picture (package-multi.py runs it).
//
//	go run build/jellytool/ico.go internal/gui/assets/icons/jellytool-app.png out.ico
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	xdraw "golang.org/x/image/draw"
)

var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run build/jellytool/ico.go <in.png> <out.ico>")
		os.Exit(2)
	}
	if err := write(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "ico:", err)
		os.Exit(1)
	}
}

func write(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	var entries [][]byte
	for _, n := range sizes {
		dst := image.NewNRGBA(image.Rect(0, 0, n, n))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
		var b bytes.Buffer
		if err := png.Encode(&b, dst); err != nil {
			return err
		}
		entries = append(entries, b.Bytes())
	}
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, n := range sizes {
		side := uint8(n) // 256 is written as 0
		binary.Write(&ico, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, Bits           uint16
			Size, Offset           uint32
		}{side, side, 0, 0, 1, 32, uint32(len(entries[i])), uint32(offset)})
		offset += len(entries[i])
	}
	for _, e := range entries {
		ico.Write(e)
	}
	return os.WriteFile(out, ico.Bytes(), 0o644)
}
