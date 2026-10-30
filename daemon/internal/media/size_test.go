package media_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

func sized(t *testing.T, mime string, b []byte, w, h uint32) {
	t.Helper()
	gw, gh, ok := media.ImageSize(mime, b)
	if !ok || gw != w || gh != h {
		t.Errorf("%s: got %d x %d ok=%v, want %d x %d", mime, gw, gh, ok, w, h)
	}
}

func TestImageSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 6, 4))
	var j, p bytes.Buffer
	jpeg.Encode(&j, img, nil)
	png.Encode(&p, img)
	sized(t, "image/jpeg", j.Bytes(), 6, 4)
	sized(t, "image/png", p.Bytes(), 6, 4)

	for o := byte(1); o <= 8; o++ {
		w, h := uint32(6), uint32(4)
		if o >= 5 {
			w, h = 4, 6
		}
		sized(t, "image/jpeg", withSegments(j.Bytes(), seg(0xE1, exifWith(o))), w, h)
	}

	// 6 x 4 pixels in each WebP form
	sized(t, "image/webp", webp(riffChunk("VP8X", "\x00\x00\x00\x00\x05\x00\x00\x03\x00\x00"), riffChunk("VP8L", "x")), 6, 4)
	sized(t, "image/webp", webp(riffChunk("VP8L", "\x2f\x05\xc0\x00\x00")), 6, 4)
	sized(t, "image/webp", webp(riffChunk("VP8 ", "\x00\x00\x00\x9d\x01\x2a\x06\x00\x04\x00")), 6, 4)

	if _, _, ok := media.ImageSize("image/webp", []byte("junk")); ok {
		t.Error("junk gave a size")
	}
}
