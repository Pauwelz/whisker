package whatsapp

import (
	"net/http"
	"net/url"
	"testing"
)

// The picture URL comes from the WhatsApp server. A plain http URL, an inner host or a look-alike host must not be fetched.
func TestPictureURLMustBeHTTPSOnAWhatsAppCDN(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://pps.whatsapp.net/v/t61/a.jpg":     true,
		"https://PPS.WhatsApp.net/a.jpg":           true,
		"https://scontent.xx.fbcdn.net/a.jpg":      true,
		"http://pps.whatsapp.net/a.jpg":            false,
		"https://whatsapp.net/a.jpg":               false,
		"https://evilwhatsapp.net/a.jpg":           false,
		"https://pps.whatsapp.net.evil.test/a.jpg": false,
		"https://evil.test/pps.whatsapp.net":       false,
		"https://pps.whatsapp.net@evil.test/a.jpg": false,
		"https://127.0.0.1/a.jpg":                  false,
		"file:///etc/passwd":                       false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := pictureURLOK(u); got != want {
			t.Errorf("pictureURLOK(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestPictureRedirectsMustStayOnTheCDN(t *testing.T) {
	redirect := func(raw string, hops int) error {
		u, _ := url.Parse(raw)
		return pictureClient.CheckRedirect(&http.Request{URL: u}, make([]*http.Request, hops))
	}
	if err := redirect("https://pps.whatsapp.net/b.jpg", 1); err != nil {
		t.Errorf("redirect inside the CDN = %v, want none", err)
	}
	for _, tc := range []struct {
		raw  string
		hops int
	}{{"http://pps.whatsapp.net/b.jpg", 1}, {"https://evil.test/b.jpg", 1}, {"https://pps.whatsapp.net/b.jpg", 10}} {
		if err := redirect(tc.raw, tc.hops); err != errPicture {
			t.Errorf("redirect to %s after %d hops = %v, want errPicture", tc.raw, tc.hops, err)
		}
	}
}
