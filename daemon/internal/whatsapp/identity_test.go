package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
)

func TestSetDeviceIdentity(t *testing.T) {
	SetDeviceIdentity("1.2.3-rc.4")
	p := store.DeviceProps
	if p.GetOs() != "whisker" || p.GetPlatformType() != waCompanionReg.DeviceProps_DESKTOP {
		t.Errorf("got %q, %v", p.GetOs(), p.GetPlatformType())
	}
	if v := p.GetVersion(); v.GetPrimary() != 1 || v.GetSecondary() != 2 || v.GetTertiary() != 3 {
		t.Errorf("version %v", v)
	}
	SetDeviceIdentity("dev") // an unparsed version must not fail
}
