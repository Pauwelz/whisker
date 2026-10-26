package whatsapp

import (
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestDeviceNameAddsTheHostname(t *testing.T) {
	if got := deviceName("fawkes"); got != "whisker (fawkes)" {
		t.Errorf("deviceName = %q", got)
	}
	if got := deviceName(""); got != "whisker" {
		t.Errorf("deviceName without a hostname = %q", got)
	}
}

func TestIdentityChangeGivesOneSystemMessageForThePhoneJID(t *testing.T) {
	for name, evt := range map[string]*events.IdentityChange{
		"phone":    {JID: types.JID{User: jef.User, Server: types.DefaultUserServer, Device: 0}, Timestamp: sysAt},
		"implicit": {JID: jef, Timestamp: sysAt, Implicit: true},
		"device":   {JID: types.JID{User: jef.User, Server: types.DefaultUserServer, Device: 2}, Timestamp: sysAt},
		"lid":      {JID: jefLID, Timestamp: sysAt},
	} {
		c := systemClient()
		c.cli.Store.LIDs = lidBook{jefLID.User: jef}
		c.handle(evt)
		close(c.events)
		var got []NewMessage
		for ev := range c.events {
			got = append(got, ev.(NewMessage))
		}
		if len(got) != 1 || got[0].Chat != jef.String() || got[0].Kind != KindDirect || got[0].Message.System == nil || got[0].Message.System.Kind != SystemIdentity || got[0].Message.Time != sysAt.Unix() {
			t.Errorf("%s: events = %+v, want one identity System Message for %s", name, got, jef)
		}
	}
}

func TestDeviceIDsRefusesAnEmptyOrFailedList(t *testing.T) {
	if _, ok := deviceIDs(nil, nil); ok {
		t.Error("an empty list was accepted")
	}
	if _, ok := deviceIDs([]types.JID{jef}, errors.New("x")); ok {
		t.Error("a failed call was accepted")
	}
	if ids, ok := deviceIDs([]types.JID{{User: "1", Device: 0}, {User: "1", Device: 3}}, nil); !ok || len(ids) != 2 || ids[1] != 3 {
		t.Errorf("ids = %v, %v", ids, ok)
	}
}
