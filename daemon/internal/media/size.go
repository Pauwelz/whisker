package media

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/jpeg" // registers the decoder for DecodeConfig
	_ "image/png"
)

// ImageSize gives the pixel size of a JPEG, PNG or WebP as a viewer shows it.
// A JPEG with Exif Orientation 5 to 8 has its width and height swapped. ok is false when the size is unknown.
func ImageSize(mime string, b []byte) (w, h uint32, ok bool) {
	switch mime {
	case "image/jpeg", "image/png":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			return 0, 0, false
		}
		w, h = uint32(cfg.Width), uint32(cfg.Height)
		if mime == "image/jpeg" && jpegOrientation(b) >= 5 {
			w, h = h, w
		}
		return w, h, true
	case "image/webp":
		return webpSize(b)
	}
	return 0, 0, false
}

// jpegOrientation reads the Orientation from the first APP1 segment before the scan. It gives 0 when there is none.
func jpegOrientation(b []byte) int {
	for i := 2; i+4 <= len(b) && b[i] == 0xFF && b[i+1] != 0xDA; {
		end := i + 2 + int(binary.BigEndian.Uint16(b[i+2:]))
		if end > len(b) || end < i+4 {
			return 0
		}
		if b[i+1] == 0xE1 {
			if o := exifOrientation(b[i+4 : end]); o != 0 {
				return o
			}
		}
		i = end
	}
	return 0
}

// webpSize reads the first VP8X, VP8L or VP8 chunk of a RIFF WebP.
func webpSize(b []byte) (w, h uint32, ok bool) {
	if len(b) < 20 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	d := b[20:]
	switch string(b[12:16]) {
	case "VP8X": // 24-bit width-1 and height-1 after the flags
		if len(d) < 10 {
			return 0, 0, false
		}
		return 1 + uint32(d[4]) | uint32(d[5])<<8 | uint32(d[6])<<16, 1 + uint32(d[7]) | uint32(d[8])<<8 | uint32(d[9])<<16, true
	case "VP8L": // 0x2F, then 14-bit width-1 and height-1
		if len(d) < 5 || d[0] != 0x2F {
			return 0, 0, false
		}
		v := binary.LittleEndian.Uint32(d[1:])
		return 1 + v&0x3FFF, 1 + v>>14&0x3FFF, true
	case "VP8 ": // frame tag, start code, then 14-bit width and height
		if len(d) < 10 || d[3] != 0x9D || d[4] != 0x01 || d[5] != 0x2A {
			return 0, 0, false
		}
		return uint32(binary.LittleEndian.Uint16(d[6:]) & 0x3FFF), uint32(binary.LittleEndian.Uint16(d[8:]) & 0x3FFF), true
	}
	return 0, 0, false
}
