package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestMediaOfThumbnail(t *testing.T) {
	thumb := []byte{0xff, 0xd8, 0xff}
	tests := []struct {
		name string
		msg  *waE2E.Message
		want []byte
	}{
		{"image", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: thumb}}, thumb},
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{JPEGThumbnail: thumb}}, thumb},
		{"document", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{JPEGThumbnail: thumb}}, thumb},
		{"image without thumbnail", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, nil},
		{"voice", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mediaOf(tt.msg).Thumbnail; string(got) != string(tt.want) {
				t.Errorf("thumbnail %v, want %v", got, tt.want)
			}
		})
	}
}
