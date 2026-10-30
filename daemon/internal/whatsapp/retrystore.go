package whatsapp

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// RetryStore keeps the Messages that whatsmeow sent, so it can answer a retry receipt after a restart.
// The chat store implements it, so it can delete a row together with its Message.
type RetryStore interface {
	// AddRetry stores a sent Message proto. targetID is the id that an edit, a revoke or a reaction acts on, or "".
	AddRetry(chat, id, targetID, format string, data []byte) error
	// Retry finds the Message in chat or altChat. A Message that is not there gives sql.ErrNoRows.
	Retry(chat, altChat, id string) (format string, data []byte, err error)
	DeleteOldRetries() error
}

// retryBuffer is the EventBuffer of the device. It keeps whatsmeow's incoming buffer and moves the outgoing events to a RetryStore.
type retryBuffer struct {
	store.EventBuffer
	retries RetryStore
}

func newRetryBuffer(inner store.EventBuffer, retries RetryStore) *retryBuffer {
	return &retryBuffer{EventBuffer: inner, retries: retries}
}

func (b *retryBuffer) AddOutgoingEvent(_ context.Context, chat types.JID, id types.MessageID, format string, plaintext []byte) error {
	target, err := retryTarget(format, plaintext)
	if err != nil {
		return err
	}
	return b.retries.AddRetry(chat.String(), id, target, format, plaintext)
}

// GetOutgoingEvent returns sql.ErrNoRows for a missing Message, as whatsmeow's own store does.
func (b *retryBuffer) GetOutgoingEvent(_ context.Context, chat, altChat types.JID, id types.MessageID) (string, []byte, error) {
	return b.retries.Retry(chat.String(), altChat.String(), id)
}

func (b *retryBuffer) DeleteOldOutgoingEvents(context.Context) error {
	return b.retries.DeleteOldRetries()
}

// retryTarget is the id that an edit or a reaction acts on. Any other Message has none.
// A revoke has none: it holds no text, and it must stay retryable after its target is gone.
func retryTarget(format string, data []byte) (string, error) {
	if format != "wa" {
		return "", nil
	}
	var m waE2E.Message
	if err := proto.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("read sent message: %w", err)
	}
	if pm := m.GetProtocolMessage(); pm.GetType() != waE2E.ProtocolMessage_REVOKE {
		if id := pm.GetKey().GetID(); id != "" {
			return id, nil
		}
	}
	return m.GetReactionMessage().GetKey().GetID(), nil
}
