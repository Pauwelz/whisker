package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// An ordinary text has no ProtocolMessage. Its zero type is REVOKE, so a missing nil check
// dropped every incoming Message as a revoke.
func TestHandleProtocolIgnoresOrdinaryMessages(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	evt := &events.Message{Message: &waE2E.Message{Conversation: proto.String("hi")}}

	if c.handleProtocol(evt) {
		t.Fatal("handleProtocol took an ordinary text as a protocol Message")
	}
	if len(c.events) != 0 {
		t.Fatalf("handleProtocol emitted %T for an ordinary text", <-c.events)
	}
}
