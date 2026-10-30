package contract_test

import (
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"

	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// A failure log holds a typed code and no error text.
func TestMediaDownloadFailureLogsTheStatus(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload("", whatsmeow.ErrMediaDownloadFailedWith403)
	emitMedia(r, "m1", media.Document, kib)
	r.nextFrame()
	r.nextFrame()
	r.send(`{"jsonrpc":"2.0","id":2,"method":"media.download","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	r.waitLog("media download failed status=403\n")
	if strings.Contains(r.errOut.String(), "status code") {
		t.Errorf("log %q holds error text", r.errOut.String())
	}
}

func TestConnectFailureLogsACode(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetConnectError(errors.New("dial tcp web.whatsapp.com"))
	r.start()
	r.waitLog("connect failed reason=unknown\n")
	if strings.Contains(r.errOut.String(), "whatsapp.com") {
		t.Errorf("log %q holds a host", r.errOut.String())
	}
}

func TestDisconnectLogsTheCode(t *testing.T) {
	r := startedRunner(t)
	r.port.Emit(whatsapp.Disconnected{Reason: whatsapp.DisconnectTemporaryBan, Code: 102})
	r.waitLog("disconnected reason=temporary-ban code=102\n")
}

func TestPresenceFailureLogsACode(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetPresenceError(whatsmeow.ErrNoPushName)
	r.start()
	r.port.Emit(whatsapp.Connected{})
	r.waitLog("presence failed reason=no-push-name\n")
}
