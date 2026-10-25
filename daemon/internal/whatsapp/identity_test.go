package whatsapp

import "testing"

func TestDeviceNameAddsTheHostname(t *testing.T) {
	if got := deviceName("fawkes"); got != "whisker (fawkes)" {
		t.Errorf("deviceName = %q", got)
	}
	if got := deviceName(""); got != "whisker" {
		t.Errorf("deviceName without a hostname = %q", got)
	}
}
