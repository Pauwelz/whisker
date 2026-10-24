package receiptpolicy

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/settings"
)

func TestDue(t *testing.T) {
	cases := []struct {
		mode     string
		trigger  Trigger
		attended bool
		want     bool
	}{
		{settings.ReadReceiptsOff, Attended, true, false},
		{settings.ReadReceiptsOff, Incoming, true, false},
		{settings.ReadReceiptsOff, Interaction, true, false},
		{settings.ReadReceiptsOnOpen, Attended, true, true},
		{settings.ReadReceiptsOnOpen, Incoming, true, true},
		{settings.ReadReceiptsOnOpen, Incoming, false, false},
		{settings.ReadReceiptsOnOpen, Interaction, true, false}, // already sent when the Chat became attended
		{settings.ReadReceiptsOnInteraction, Attended, true, false},
		{settings.ReadReceiptsOnInteraction, Incoming, true, false},
		{settings.ReadReceiptsOnInteraction, Interaction, true, true},
		{"unknown", Interaction, true, false},
	}
	for _, c := range cases {
		if got := Due(c.mode, c.trigger, c.attended); got != c.want {
			t.Errorf("Due(%q, %v, %v) = %v, want %v", c.mode, c.trigger, c.attended, got, c.want)
		}
	}
}
