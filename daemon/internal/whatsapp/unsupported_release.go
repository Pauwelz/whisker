//go:build !dev

package whatsapp

import "go.mau.fi/whatsmeow/proto/waE2E"

// logUnsupported does nothing in a release build. A dev build (-tags dev) logs the type of the Message.
func logUnsupported(string, *waE2E.Message) {}
