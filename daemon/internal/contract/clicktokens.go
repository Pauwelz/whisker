package contract

import (
	"crypto/rand"
	"encoding/hex"
)

// maxClickTokens caps the map. The oldest token goes out first, and its notification then opens the Chat list.
const maxClickTokens = 256

// clickTokens maps the opaque token of a notification click to its Chat. The click command holds the token,
// so the Chat id never goes into the arguments of a process or into the notification history of Omarchy.
// It lives on the Daemon loop only, and a restart of the Daemon forgets it.
type clickTokens struct {
	byChat  map[string]string
	byToken map[string]string
	order   []string // the Chats, oldest token first
}

// tokenFor returns the token of chat, and makes one when the Chat has none. A Chat keeps its token.
func (c *clickTokens) tokenFor(chat string) string {
	if t, ok := c.byChat[chat]; ok {
		return t
	}
	if c.byChat == nil {
		c.byChat, c.byToken = map[string]string{}, map[string]string{}
	}
	if len(c.order) >= maxClickTokens {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.byToken, c.byChat[old])
		delete(c.byChat, old)
	}
	b := make([]byte, 16)
	rand.Read(b) // never fails, since Go 1.24
	t := hex.EncodeToString(b)
	c.byChat[chat], c.byToken[t] = t, chat
	c.order = append(c.order, chat)
	return t
}

// chatOf returns the Chat of a token, or false for a token the Daemon does not know.
func (c *clickTokens) chatOf(token string) (string, bool) {
	chat, ok := c.byToken[token]
	return chat, ok
}
