//go:build dev

package whatsapp

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestUnsupportedLineNamesTheFieldsAndThePairedMediaType(t *testing.T) {
	m := &waE2E.Message{
		AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption:     proto.String("secret caption"),
			ContextInfo: &waE2E.ContextInfo{PairedMediaType: waE2E.ContextInfo_HD_IMAGE_CHILD.Enum(), Participant: proto.String("111@s.whatsapp.net")},
		}}},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: []byte("key")},
	}
	got := unsupportedLine("ABC", m)
	want := "unsupported message ABC: associatedChildMessage(pairedMediaType=HD_IMAGE_CHILD) messageContextInfo"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	for _, secret := range []string{"secret caption", "111", "key"} {
		if strings.Contains(strings.TrimPrefix(got, "unsupported message ABC: "), secret) {
			t.Errorf("line holds %q", secret)
		}
	}
}
