package whatsapp

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"go.mau.fi/whatsmeow"
)

// A sent image carries its pixel size. The size comes from a prefix of the file, which is rewound for the upload.
func TestSentImageCarriesItsSize(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 6, 4))); err != nil {
		t.Fatal(err)
	}
	plain := bytes.NewReader(buf.Bytes())
	w, h, err := imageSize("image/png", plain)
	if err != nil {
		t.Fatal(err)
	}
	msg := imageMessage(whatsmeow.UploadResponse{URL: "u"}, "image/png", "cap", w, h)
	if msg.GetWidth() != 6 || msg.GetHeight() != 4 || msg.GetCaption() != "cap" {
		t.Errorf("message = %v, want 6 x 4 with the caption", msg)
	}
	if pos, _ := plain.Seek(0, 1); pos != 0 {
		t.Errorf("plain is at %d, want 0", pos)
	}
}

// An image of unknown size goes out without one.
func TestSentImageOfUnknownSizeHasNone(t *testing.T) {
	w, h, err := imageSize("image/png", bytes.NewReader([]byte("junk")))
	if err != nil || w != nil || h != nil {
		t.Errorf("size = %v x %v, err = %v, want none", w, h, err)
	}
	if msg := imageMessage(whatsmeow.UploadResponse{}, "image/png", "", w, h); msg.Width != nil || msg.Height != nil {
		t.Errorf("message = %v, want no size", msg)
	}
}
