package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

// pairTimeout bounds the PairPhone network call. sendTimeout bounds one Send.
// editTimeout bounds one Edit, Revoke or App State write, which block the Daemon loop.
// storeTimeout bounds one call to the LID store.
const (
	pairTimeout = 30 * time.Second
	sendTimeout = 90 * time.Second
	// shortcut: one fixed limit for every download, upgrade to a size based limit if a large video times out.
	downloadTimeout = 10 * time.Minute
	// thumbnailTimeout bounds one separate thumbnail, which is at most MaxThumbnailBytes.
	thumbnailTimeout = 30 * time.Second
	editTimeout      = 15 * time.Second
	uploadTimeout    = 10 * time.Minute
	storeTimeout     = 5 * time.Second
)

// Client is the whatsmeow adapter: the only place whatsmeow types appear.
// Tests never use it: it would reach the WhatsApp servers.
type Client struct {
	cli    *whatsmeow.Client
	events chan Event
	mu     sync.Mutex // one Connect at a time

	syncMu  sync.Mutex
	pending []Event // the flag events of a full App State sync, until it completes

	// lidGroups holds the addressing mode of each Group Chat that a live event or a joined-group event showed: true for LID.
	// shortcut: only the current run knows it, so the first reply or Reaction in a group after a start, before any live event of it, still sends the phone JID. Upgrade with GetGroupInfo.
	lidGroups sync.Map
}

// SetDeviceIdentity names the linked device "whisker (hostname)" on the phone. Call it once, before the first connect and pair.
// WhatsApp keeps the name it got at pairing. A version that is not "major.minor.patch" at its start counts as 0.0.0.
func SetDeviceIdentity(version string) {
	var v [3]uint32
	fmt.Sscanf(version, "%d.%d.%d", &v[0], &v[1], &v[2])
	host, _ := os.Hostname()
	store.SetOSInfo(deviceName(host), v)
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
}

// deviceName is "whisker (host)", or "whisker" when the hostname is unknown.
func deviceName(host string) string {
	if host == "" {
		return "whisker"
	}
	return "whisker (" + host + ")"
}

// NewClient loads the device from the device store (a new unpaired device if there is none).
func NewClient(container *sqlstore.Container) (*Client, error) {
	dev, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, err
	}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop), events: make(chan Event, 64)}
	// The first App State sync after a link is a full sync. Without this, its Pin, Archive, Mute and Read Marker events are lost.
	c.cli.EmitAppStateEventsOnFullSync = true
	c.cli.AddEventHandler(c.handle)
	return c, nil
}

func (c *Client) Linked() bool         { return c.cli.Store.ID != nil }
func (c *Client) Events() <-chan Event { return c.events }
func (c *Client) OwnUsers() []string {
	if c.cli.Store.ID == nil {
		return nil
	}
	return []string{c.cli.Store.GetJID().User, c.cli.Store.GetLID().User}
}

// isOwn tells that a JID is a device of the user, by phone JID or LID.
func (c *Client) isOwn(jid types.JID) bool {
	return jid.User != "" && slices.Contains(c.OwnUsers(), jid.User)
}

func (c *Client) GroupParticipants(chat string) ([]Participant, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	if !c.cli.IsConnected() {
		return nil, ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	info, err := c.cli.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, offline(err)
	}
	var participants []Participant
	for _, p := range info.Participants {
		m := Participant{Phone: p.PhoneNumber.ToNonAD().String(), LID: p.LID.ToNonAD().String()}
		// The primary JID is one of the two IDs: fill the one that the fields leave empty.
		switch p.JID.Server {
		case types.DefaultUserServer:
			m.Phone = p.JID.ToNonAD().String()
		case types.HiddenUserServer:
			m.LID = p.JID.ToNonAD().String()
		}
		if !c.isOwn(p.JID) && !c.isOwn(p.PhoneNumber) && !c.isOwn(p.LID) {
			participants = append(participants, m)
		}
	}
	return participants, nil
}

func (c *Client) Logout() error {
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	return c.cli.Logout(ctx)
}
func (c *Client) Disconnect() { c.cli.Disconnect() }

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cli.Disconnect() // a retry starts from a closed socket
	if c.Linked() {
		return c.cli.Connect()
	}
	qrs, err := c.cli.GetQRChannel(context.Background())
	if err != nil {
		return err
	}
	if err := c.cli.Connect(); err != nil {
		return err
	}
	go c.forwardQRs(qrs)
	return nil
}

func (c *Client) forwardQRs(qrs <-chan whatsmeow.QRChannelItem) {
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			c.events <- QRCode{Code: item.Code}
		case whatsmeow.QRChannelSuccess.Event:
			// events.PairSuccess reports success.
		default:
			c.events <- LinkFailed{Reason: qrFailureReason(item)}
		}
	}
}

func qrFailureReason(item whatsmeow.QRChannelItem) string {
	switch item {
	case whatsmeow.QRChannelTimeout:
		return ReasonTimeout
	case whatsmeow.QRChannelClientOutdated:
		return ReasonClientOutdated
	case whatsmeow.QRChannelScannedWithoutMultidevice:
		return ReasonScannedWithoutMultidevice
	}
	return ReasonUnexpected
}

func (c *Client) PairCode(phone string) (string, error) {
	digits, err := cleanPhone(phone)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	return c.cli.PairPhone(ctx, digits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
}

func (c *Client) PhoneOf(lid string) string {
	jid, err := types.ParseJID(lid)
	if err != nil || jid.Server != types.HiddenUserServer {
		return ""
	}
	pn, err := c.lookupPhone(jid)
	if err != nil || pn.IsEmpty() {
		return ""
	}
	return pn.ToNonAD().String()
}

func (c *Client) Lookup(phone string) (string, string, error) {
	digits, err := cleanPhone(phone)
	if err != nil {
		return "", "", err
	}
	if !c.cli.IsConnected() {
		return digits, "", ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	found, err := c.cli.IsOnWhatsApp(ctx, []string{digits})
	if err != nil {
		return digits, "", offline(err)
	}
	for _, r := range found {
		if r.IsIn && r.Query == digits {
			if !r.PhoneNumber.IsEmpty() { // a LID answer still names the phone ID, the key of the Chat
				return digits, r.PhoneNumber.String(), nil
			}
			return digits, r.JID.String(), nil
		}
	}
	return digits, "", nil
}

// maxPictureBytes caps the preview download. A preview is a few KiB, so a larger body is not one.
const maxPictureBytes = 512 << 10

// errPicture is the one error of a failed picture download. Its text holds no URL.
var errPicture = errors.New("picture download failed")

// pictureURLOK tells that a picture URL is https on a WhatsApp CDN host. The URL comes from the server, so the Daemon checks it before it fetches.
func pictureURLOK(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (strings.HasSuffix(host, ".whatsapp.net") || strings.HasSuffix(host, ".fbcdn.net"))
}

// pictureClient fetches pictures. A redirect must also stay on a WhatsApp CDN host. It keeps the limit of 10 redirects of the default client.
var pictureClient = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 || !pictureURLOK(req.URL) {
		return errPicture
	}
	return nil
}}

func (c *Client) ProfilePicture(chat, knownID string) (string, []byte, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return "", nil, err
	}
	if !c.cli.IsConnected() {
		return "", nil, ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	info, err := c.cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true, ExistingID: knownID})
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		return "", nil, nil
	case err != nil:
		return "", nil, offline(err)
	case info == nil: // the known picture is still current
		return knownID, nil, nil
	}
	u, err := url.Parse(info.URL)
	if err != nil || !pictureURLOK(u) {
		return "", nil, errPicture
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", nil, errPicture
	}
	resp, err := pictureClient.Do(req)
	if err != nil {
		return "", nil, errPicture // the error text holds the URL
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, errPicture
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPictureBytes+1))
	if err != nil || len(data) > maxPictureBytes {
		return "", nil, errPicture
	}
	return info.ID, data, nil
}

func (c *Client) messageOf(info types.MessageInfo, m *waE2E.Message, body content) Message {
	return Message{
		ID: info.ID, Sender: c.senderOf(info.MessageSource).String(), FromMe: info.IsFromMe, Time: info.Timestamp.Unix(),
		Text: body.Text, PushName: info.PushName, Quote: c.quoteOf(m), Media: body.Media, Timer: timerOf(m),
		Mentions: c.phoneStrings(MentionsIn(body.Text, body.Mentions)), MentionsMe: mentionsUser(body.Mentions, c.cli.Store.GetJID().User, c.cli.Store.GetLID().User),
	}
}

func (c *Client) kindOf(jid types.JID) string {
	switch {
	case jid.Server == types.GroupServer:
		return KindGroup
	case c.cli.Store.ID != nil && jid.User == c.cli.Store.ID.User:
		return KindSelf
	}
	return KindDirect
}

// quoteOf is the Quote that a Message carries, or nil. The user's own JID maps to the empty sender.
func (c *Client) quoteOf(m *waE2E.Message) *Quote {
	ctx := m.GetExtendedTextMessage().GetContextInfo()
	if ctx.GetStanzaID() == "" || ReservedID(ctx.GetStanzaID()) {
		return nil
	}
	sender := c.phoneString(ctx.GetParticipant())
	if jid, err := types.ParseJID(sender); err == nil && c.cli.Store.ID != nil && jid.User == c.cli.Store.ID.User {
		sender = ""
	}
	return &Quote{ID: ctx.GetStanzaID(), Sender: sender, Text: messageText(ctx.GetQuotedMessage())}
}

// messageText is the text of a Message, or the caption of its Media. System Messages have none yet.
func messageText(m *waE2E.Message) string {
	if text := m.GetConversation(); text != "" {
		return text
	}
	if text := m.GetExtendedTextMessage().GetText(); text != "" {
		return text
	}
	switch {
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption()
	}
	return m.GetDocumentMessage().GetCaption()
}

// sizeOf is the declared size of Media. A value above MaxInt64 is unknown (0), never negative.
func sizeOf(n uint64) int64 {
	if n > math.MaxInt64 {
		return 0
	}
	return int64(n)
}

// mediaOf maps the Media of a Message to the port type, or nil when the Message has none.
func mediaOf(m *waE2E.Message) *Media {
	switch {
	case m.GetImageMessage() != nil:
		im := m.GetImageMessage()
		return &Media{Kind: media.Image, Mime: im.GetMimetype(), Size: sizeOf(im.GetFileLength()), Key: im.GetMediaKey(), DirectPath: im.GetDirectPath(), FileSHA256: im.GetFileSHA256(), FileEncSHA256: im.GetFileEncSHA256(), Thumbnail: im.GetJPEGThumbnail(),
			ThumbDirectPath: im.GetThumbnailDirectPath(), ThumbSHA256: im.GetThumbnailSHA256(), ThumbEncSHA256: im.GetThumbnailEncSHA256()}
	case m.GetStickerMessage() != nil:
		sm := m.GetStickerMessage()
		return &Media{Kind: media.Sticker, Mime: sm.GetMimetype(), Size: sizeOf(sm.GetFileLength()), Key: sm.GetMediaKey(), DirectPath: sm.GetDirectPath(), FileSHA256: sm.GetFileSHA256(), FileEncSHA256: sm.GetFileEncSHA256()}
	case m.GetAudioMessage() != nil:
		am := m.GetAudioMessage()
		kind := media.Audio
		if am.GetPTT() {
			kind = media.Voice
		}
		return &Media{Kind: kind, Mime: am.GetMimetype(), Size: sizeOf(am.GetFileLength()), Key: am.GetMediaKey(), DirectPath: am.GetDirectPath(), FileSHA256: am.GetFileSHA256(), FileEncSHA256: am.GetFileEncSHA256(), Duration: int64(am.GetSeconds())}
	case m.GetVideoMessage() != nil:
		vm := m.GetVideoMessage()
		return &Media{Kind: media.Video, Mime: vm.GetMimetype(), Size: sizeOf(vm.GetFileLength()), Key: vm.GetMediaKey(), DirectPath: vm.GetDirectPath(), FileSHA256: vm.GetFileSHA256(), FileEncSHA256: vm.GetFileEncSHA256(), Thumbnail: vm.GetJPEGThumbnail(), Duration: int64(vm.GetSeconds()), Gif: vm.GetGifPlayback(),
			ThumbDirectPath: vm.GetThumbnailDirectPath(), ThumbSHA256: vm.GetThumbnailSHA256(), ThumbEncSHA256: vm.GetThumbnailEncSHA256()}
	case m.GetDocumentMessage() != nil:
		dm := m.GetDocumentMessage()
		return &Media{Kind: media.Document, Mime: dm.GetMimetype(), Name: CleanName(dm.GetFileName(), 0), Size: sizeOf(dm.GetFileLength()), Key: dm.GetMediaKey(), DirectPath: dm.GetDirectPath(), FileSHA256: dm.GetFileSHA256(), FileEncSHA256: dm.GetFileEncSHA256(), Thumbnail: dm.GetJPEGThumbnail(),
			ThumbDirectPath: dm.GetThumbnailDirectPath(), ThumbSHA256: dm.GetThumbnailSHA256(), ThumbEncSHA256: dm.GetThumbnailEncSHA256()}
	}
	return nil
}

func (c *Client) Download(ref Media, dst *os.File, limit int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	return c.cli.DownloadMediaWithPathToFile(ctx, ref.DirectPath, ref.FileEncSHA256, ref.FileSHA256, ref.Key, whatsmeow.MediaType(ref.Kind.Info()), "", false, capDisk{file: dst, max: limit})
}

// DownloadThumbnail fails when the body is above MaxThumbnailBytes. It uses the media key and key type of the file itself; whatsmeow's own DownloadThumbnail knows only link previews.
func (c *Client) DownloadThumbnail(ref Media) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailTimeout)
	defer cancel()
	f := &capFile{}
	// whatsmeow's DownloadMediaWithPath reads the whole body into memory, so a capped File takes the stream.
	err := c.cli.DownloadMediaWithPathToFile(ctx, ref.ThumbDirectPath, ref.ThumbEncSHA256, ref.ThumbSHA256, ref.Key, whatsmeow.MediaType(ref.Kind.Info()), "", false, f)
	if err != nil {
		return nil, err
	}
	return f.buf, nil
}

// phone is the phone JID of a user JID that whatsmeow gives out. Any alt that holds the phone JID wins over the LID store.
func (c *Client) phone(jid types.JID, alts ...types.JID) types.JID {
	return resolvePhone(jid, c.lookupPhone, alts...)
}

func (c *Client) lookupPhone(lid types.JID) (types.JID, error) {
	if own := c.cli.Store.ID; own != nil && lid.User == c.cli.Store.GetLID().User {
		return own.ToNonAD(), nil
	}
	if c.cli.Store.LIDs == nil {
		return types.EmptyJID, errors.New("no LID store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return c.cli.Store.LIDs.GetPNForLID(ctx, lid)
}

// phoneOfConversation is the phone JID that a History Sync conversation carries, or the empty JID.
func phoneOfConversation(s string) types.JID {
	jid, err := types.ParseJID(s)
	if err != nil {
		return types.EmptyJID
	}
	if jid.Server == types.LegacyUserServer {
		jid.Server = types.DefaultUserServer
	}
	if jid.Server != types.DefaultUserServer {
		return types.EmptyJID
	}
	return jid.ToNonAD()
}

// rememberPhone stores a LID and its phone JID, so live events for that LID find the same Chat.
// A failed write only costs the mapping: the lookup then falls back to the LID.
func (c *Client) rememberPhone(lid, pn types.JID) {
	if c.cli.Store.LIDs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	_ = c.cli.Store.LIDs.PutLIDMapping(ctx, lid.ToNonAD(), pn)
}

// phoneString is phone for a JID string. A string that does not parse stays as it is.
func (c *Client) phoneString(s string) string {
	jid, err := types.ParseJID(s)
	if err != nil || jid.IsEmpty() {
		return s
	}
	return c.phone(jid).String()
}

func (c *Client) phoneStrings(in []string) []string {
	var out []string
	for _, s := range in {
		out = append(out, c.phoneString(s))
	}
	return out
}

// chatOf is the phone JID of the Chat of an event. In a Direct Chat the alt of the other user is the sender alt, or the recipient alt for the user's own Messages.
func (c *Client) chatOf(src types.MessageSource) types.JID {
	alt := src.SenderAlt
	if src.IsFromMe {
		alt = src.RecipientAlt
	}
	return c.phone(src.Chat, alt)
}

func (c *Client) senderOf(src types.MessageSource) types.JID {
	return c.phone(src.Sender, src.SenderAlt)
}

// resolvePhone maps a LID to the phone JID: first an alt that is a phone JID, then lookup. Other JIDs lose only their device part.
// shortcut: an unknown LID stays as it is, so its Messages open a separate Chat.
func resolvePhone(jid types.JID, lookup func(types.JID) (types.JID, error), alts ...types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	for _, alt := range alts {
		if alt.Server == types.DefaultUserServer {
			return alt.ToNonAD()
		}
	}
	if pn, err := lookup(jid); err == nil && !pn.IsEmpty() {
		return pn.ToNonAD()
	}
	return jid
}
