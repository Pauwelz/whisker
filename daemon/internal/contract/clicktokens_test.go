package contract

import (
	"fmt"
	"regexp"
	"testing"
)

func TestClickTokenIsRandomHexAndReused(t *testing.T) {
	var c clickTokens
	a := c.tokenFor("1@s.whatsapp.net")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(a) {
		t.Errorf("token = %q, want 32 hex digits", a)
	}
	if c.tokenFor("1@s.whatsapp.net") != a {
		t.Error("a second call for the same Chat gave another token")
	}
	if b := c.tokenFor("2@s.whatsapp.net"); b == a {
		t.Error("two Chats share a token")
	}
	if chat, ok := c.chatOf(a); !ok || chat != "1@s.whatsapp.net" {
		t.Errorf("chatOf = %q, %v", chat, ok)
	}
}

func TestClickTokensDropTheOldestAtTheCap(t *testing.T) {
	var c clickTokens
	first := c.tokenFor("chat0")
	for i := 1; i <= maxClickTokens; i++ {
		c.tokenFor(fmt.Sprintf("chat%d", i))
	}
	if _, ok := c.chatOf(first); ok {
		t.Error("the oldest token is still known")
	}
	if len(c.byToken) != maxClickTokens || len(c.byChat) != maxClickTokens {
		t.Errorf("map sizes = %d, %d, want %d", len(c.byToken), len(c.byChat), maxClickTokens)
	}
}
