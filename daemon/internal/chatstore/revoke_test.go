package chatstore_test

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// Only the author revokes a Message: a wrong sender or a wrong fromMe leaves the text and the Media as they are.
func TestRevokeByANonAuthorChangesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		sender string
		fromMe bool
	}{
		"another sender":           {bobJID, false},
		"the user for another's":   {"", true},
		"the author, fromMe wrong": {annJID, true},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newStore(t)
			live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Text: "real", Media: pic("t")})
			removed := 0
			s.RemoveMedia = func(string, string) { removed++ }
			changed, err := s.Revoke(colGroup, "x", tc.sender, tc.fromMe)
			if err != nil || changed {
				t.Fatalf("Revoke = %v, %v; want false, nil", changed, err)
			}
			m, err := s.Message(colGroup, "x")
			if err != nil {
				t.Fatal(err)
			}
			if m.Revoked || m.Text != "real" || m.Media == nil || removed != 0 {
				t.Errorf("Message = %+v, removed %d; want it unchanged", m, removed)
			}
		})
	}
}

func TestRevokeByTheAuthorMakesATombstoneAndDeletesTheMedia(t *testing.T) {
	for name, tc := range map[string]struct {
		msg    whatsapp.Message
		sender string
		fromMe bool
	}{
		"participant": {whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Text: "real", Media: pic("t")}, annJID, false},
		"the user":    {whatsapp.Message{ID: "x", FromMe: true, Time: 1, Text: "real", Media: pic("t")}, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newStore(t)
			live(t, s, tc.msg)
			removed := 0
			s.RemoveMedia = func(string, string) { removed++ }
			changed, err := s.Revoke(colGroup, "x", tc.sender, tc.fromMe)
			if err != nil || !changed {
				t.Fatalf("Revoke = %v, %v; want true, nil", changed, err)
			}
			m, _ := s.Message(colGroup, "x")
			if !m.Revoked || m.Text != "" || m.Media != nil || removed != 1 {
				t.Errorf("Message = %+v, removed %d; want a Tombstone without Media", m, removed)
			}
		})
	}
}

func TestRevokeByAdminMakesAnAdminTombstoneOnlyForTheAuthor(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Text: "real", Media: pic("t")})
	if changed, err := s.RevokeByAdmin(colGroup, "x", bobJID); err != nil || changed {
		t.Fatalf("RevokeByAdmin(not the author) = %v, %v; want false, nil", changed, err)
	}
	if changed, err := s.RevokeByAdmin(colGroup, "x", annJID); err != nil || !changed {
		t.Fatalf("RevokeByAdmin = %v, %v; want true, nil", changed, err)
	}
	m, _ := s.Message(colGroup, "x")
	if !m.Revoked || !m.RevokedByAdmin || m.Text != "" || m.Media != nil {
		t.Errorf("Message = %+v; want an admin Tombstone without Media", m)
	}
}
