package imagemcp

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	pngenc "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
)

// The middle square is translucent green; stretching instead of cropping
// leaves red at the sides (or the top and bottom).
func resizeFixture(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			c := color.NRGBA{G: 200, A: 128}
			if width > height && (x < (width-height)/2 || x >= (width+height)/2) ||
				height > width && (y < (height-width)/2 || y >= (height+width)/2) {
				c = color.NRGBA{R: 255, A: 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&b, im, &jpeg.Options{Quality: 95})
	} else {
		err = pngenc.Encode(&b, im)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestGenerateSavesExactSize(t *testing.T) {
	for _, viaURL := range []bool{false, true} {
		for _, edit := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "base64", true: "url"}[viaURL], map[bool]string{false: "generation", true: "edit"}[edit]}, "/"), func(t *testing.T) {
				images := [][]byte{resizeFixture(t, "png", 16, 8), resizeFixture(t, "jpeg", 8, 16)}
				files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/first" {
						w.Write(images[0])
					} else {
						w.Write(images[1])
					}
				}))
				t.Cleanup(files.Close)
				if viaURL {
					fetchLocal(t)
				}
				g := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == gateway.ImageMCPPath {
						b, _ := io.ReadAll(r.Body)
						modelsMCP(w, b)
						return
					}
					var req map[string]any
					json.NewDecoder(r.Body).Decode(&req)
					endpoint := "/v1/images/generations"
					if edit {
						endpoint = "/v1/images/edits"
					}
					if r.URL.Path != endpoint || req["size"] != "16x16" || req["model"] != "art/painter" || req["n"] != float64(2) || req["response_format"] != "b64_json" {
						t.Errorf("request changed: %s %v", r.URL.Path, req)
					}
					var data []map[string]string
					for i, img := range images {
						d := map[string]string{"b64_json": base64.StdEncoding.EncodeToString(img), "mime_type": "image/png"}
						if viaURL {
							d = map[string]string{"url": files.URL + []string{"/first", "/second"}[i]}
						}
						data = append(data, d)
					}
					json.NewEncoder(w).Encode(map[string]any{"model": "art/painter", "data": data})
				})
				project := t.TempDir()
				c := initialized(t, g, project)
				a := map[string]any{"prompt": "green square", "size": "16x16", "path": "image.png", "model": "art/painter", "n": 2}
				if edit {
					a["reference_images"] = []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(images[0])}
				}
				r, out := c.await(a)
				if r["isError"] == true {
					t.Fatal(out)
				}
				for i, ext := range []string{".png", ".jpg"} {
					path := filepath.Join(project, []string{"image-1", "image-2"}[i]+ext)
					b, err := os.ReadFile(path)
					if err != nil {
						t.Fatalf("%s: %v\n%s", path, err, out)
					}
					im, format, err := image.Decode(bytes.NewReader(b))
					if err != nil {
						t.Fatal(err)
					}
					if im.Bounds().Size() != image.Pt(16, 16) || format != []string{"png", "jpeg"}[i] {
						t.Fatalf("saved image %d: size %v, format %s", i+1, im.Bounds().Size(), format)
					}
					p := []image.Point{image.Pt(1, 8), image.Pt(8, 1)}[i]
					red, green, _, _ := im.At(p.X, p.Y).RGBA()
					_, _, _, alpha := im.At(8, 8).RGBA()
					if green <= red || i == 0 && (alpha < 32000 || alpha > 33500) {
						t.Fatalf("image %d lost center crop or transparency: %v", i+1, im.At(p.X, p.Y))
					}
				}
				for _, want := range []string{"16x8", "8x16", "16x16", "locally resized", "center crop"} {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q in result: %s", want, out)
					}
				}
			})
		}
	}
}

func TestGenerateKeepsOriginalSizeBytes(t *testing.T) {
	for _, size := range []string{"", "auto", "16:9", "16x8"} {
		t.Run(size, func(t *testing.T) {
			original := resizeFixture(t, "png", 16, 8)
			g := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(original)}}})
			})
			project := t.TempDir()
			c := initialized(t, g, project)
			r, out := c.await(map[string]any{"prompt": "green", "size": size, "path": "original.png"})
			got, err := os.ReadFile(filepath.Join(project, "original.png"))
			if r["isError"] == true || err != nil || !bytes.Equal(got, original) {
				t.Fatalf("original changed: %v, %s", err, out)
			}
		})
	}
}

func TestResizeReportedDimensions(t *testing.T) {
	for _, tc := range []struct {
		from, to image.Point
	}{
		{image.Pt(1672, 941), image.Pt(2560, 1440)},
		{image.Pt(80, 40), image.Pt(20, 20)},
	} {
		original := resizeFixture(t, "png", tc.from.X, tc.from.Y)
		got, detail, err := resizeImage(original, tc.to)
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(got))
		if err != nil || image.Pt(cfg.Width, cfg.Height) != tc.to || !strings.Contains(detail, "locally resized") {
			t.Fatalf("size %dx%d, error %v, result %s", cfg.Width, cfg.Height, err, detail)
		}
	}
}

func TestGenerateRejectsUnsafeSizeBeforeDrawing(t *testing.T) {
	var calls atomic.Int32
	g := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == gateway.ImageMCPPath {
			b, _ := io.ReadAll(r.Body)
			modelsMCP(w, b)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	})
	c := initialized(t, g, t.TempDir())
	for _, size := range []string{"0x1024", "1024x0", "16385x1", "16384x2049", "999999999999999999999999x1"} {
		r, out := c.await(map[string]any{"prompt": "green", "size": size})
		if r["isError"] != true || !strings.Contains(out, "pixels per side") {
			t.Errorf("size %s: %v", size, r)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unsafe sizes reached the paid image API %d times", calls.Load())
	}
	for _, size := range []string{"1x1", "16384x2048", " 2560X1440 "} {
		if got, err := pixelSize(size); err != nil || got == (image.Point{}) {
			t.Errorf("valid boundary %s refused: %v", size, err)
		}
	}
}

// Only the IHDR is enlarged, so rejecting it must happen before decoding
// pixels; otherwise this fails with a corrupt-image error instead.
func oversizedPNG(t *testing.T, width, height uint32) []byte {
	t.Helper()
	b := resizeFixture(t, "png", 8, 8)
	binary.BigEndian.PutUint32(b[16:20], width)
	binary.BigEndian.PutUint32(b[20:24], height)
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestResizeBoundsDecodedMemory(t *testing.T) {
	for _, tc := range []struct {
		width, height uint32
		to            image.Point
	}{
		{16385, 1, image.Pt(16, 16)},
		{8192, 8192, image.Pt(16, 16)},
		{8192, 4096, image.Pt(8192, 4095)},
	} {
		_, _, err := resizeImage(oversizedPNG(t, tc.width, tc.height), tc.to)
		if err == nil || !strings.Contains(err.Error(), "memory limit") {
			t.Errorf("%dx%d: %v", tc.width, tc.height, err)
		}
	}
}

func TestGenerateKeepsOriginalWhenResizingFails(t *testing.T) {
	var gifBytes bytes.Buffer
	if err := gif.Encode(&gifBytes, image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, ext string
		data      []byte
	}{
		{"broken", ".png", png},
		{"unsupported", ".gif", gifBytes.Bytes()},
		{"oversized", ".png", oversizedPNG(t, 16385, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tc.data)}}})
			})
			project := t.TempDir()
			c := initialized(t, g, project)
			r, out := c.await(map[string]any{"prompt": "green", "size": "16x16", "path": "original.png"})
			b, err := os.ReadFile(filepath.Join(project, "original"+tc.ext))
			if r["isError"] == true || err != nil || !bytes.Equal(b, tc.data) ||
				!strings.Contains(out, "local resizing to 16x16 failed") || !strings.Contains(out, "saved original without resizing") {
				t.Fatalf("paid image lost or failure hidden: %v, %s", err, out)
			}
		})
	}
}
