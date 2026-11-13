package imagemcp

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	pngenc "image/png"
	"regexp"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
	_ "golang.org/x/image/webp"
)

// Bound decoded pixels and the estimated working set before allocating.
// Transform draws straight into the target, without a larger cover canvas
// or Scale's width-by-source-height floating-point intermediate.
const (
	imageMaxSide   = 16384
	imageMaxPixels = 32 << 20
	imageMaxMemory = 512 << 20
)

var exactSize = regexp.MustCompile(`^([0-9]+)[xX]([0-9]+)$`)

// pixelSize leaves ratios and model-specific size names to the provider.
func pixelSize(size string) (image.Point, error) {
	m := exactSize.FindStringSubmatch(strings.TrimSpace(size))
	if m == nil {
		return image.Point{}, nil
	}
	w, werr := strconv.Atoi(m[1])
	h, herr := strconv.Atoi(m[2])
	if werr != nil || herr != nil || !imageSizeAllowed(w, h) {
		return image.Point{}, fmt.Errorf("image size %q must have 1–%d pixels per side and at most %d pixels in total", size, imageMaxSide, imageMaxPixels)
	}
	return image.Pt(w, h), nil
}

func imageSizeAllowed(w, h int) bool {
	return w > 0 && h > 0 && w <= imageMaxSide && h <= imageMaxSide && int64(w)*int64(h) <= imageMaxPixels
}

// resizeImage enforces an explicit pixel size for local MCP files only.
// Equal sizes keep their exact bytes. A caller keeps the original on error
// and says why: a paid-for image must not disappear on a processing failure.
func resizeImage(data []byte, size image.Point) ([]byte, string, error) {
	if size == (image.Point{}) {
		return data, "", nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("read image dimensions: %w", err)
	}
	original := fmt.Sprintf("original %dx%d", cfg.Width, cfg.Height)
	if cfg.Width == size.X && cfg.Height == size.Y {
		return data, original + "; saved unchanged", nil
	}
	if format != "png" && format != "jpeg" {
		return nil, "", fmt.Errorf("%s: local resizing supports PNG and JPEG, received %s", original, format)
	}
	// PNG may decode to 8 bytes per pixel. Allow another 12 per output
	// pixel for RGBA, the encoder and its growing output buffer.
	if !imageSizeAllowed(cfg.Width, cfg.Height) || !imageSizeAllowed(size.X, size.Y) ||
		int64(cfg.Width)*int64(cfg.Height)*8+int64(size.X)*int64(size.Y)*12+int64(len(data)) > imageMaxMemory {
		return nil, "", fmt.Errorf("%s: local resizing exceeds the pixel or 512 MiB working-memory limit", original)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("%s: decode image: %w", original, err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	scale := max(float64(size.X)/float64(cfg.Width), float64(size.Y)/float64(cfg.Height))
	x := (float64(size.X) - scale*float64(cfg.Width)) / 2
	y := (float64(size.Y) - scale*float64(cfg.Height)) / 2
	xdraw.CatmullRom.Transform(dst, f64.Aff3{scale, 0, x, 0, scale, y}, src, src.Bounds(), xdraw.Src, nil)
	var out bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 95})
	} else {
		err = pngenc.Encode(&out, dst)
	}
	if err != nil {
		return nil, "", fmt.Errorf("%s: encode image: %w", original, err)
	}
	return out.Bytes(), fmt.Sprintf("%s; locally resized to %dx%d (interpolation, aspect ratio preserved, center crop)", original, size.X, size.Y), nil
}
