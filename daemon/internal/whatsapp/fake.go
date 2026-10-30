package whatsapp

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
)

// Fake is a scripted Port for tests. A test calls Emit to play WhatsApp events.
type Fake struct {
	events chan Event

	mu             sync.Mutex
	linked         bool
	pairCode       string
	pairErr        error
	connectErr     error
	connects       int
	sendErr        error
	presenceErr    error
	sent           []string
	uploads        []string
	timers         []int64
	attempts       int
	ids            int
	logoutErr      error
	logouts        int
	content        string
	dlErr          error
	expired        []string
	retries        []string
	retryErr       error
	downloads      int
	dlDelay        time.Duration
	dlActive       int
	dlPeak         int
	thumb          string
	thumbErr       error
	thumbDownloads int
	thumbDelay     time.Duration
	thumbActive    int
	thumbPeak      int
	readErr        error
	reads          []Read
	chatReads      []string
	pins           []string
	mutes          []string
	unreads        []string
	deletes        []string
	plays          []Read
	selfReads      []Read
	edits          []string
	revokes        []string
	quotes         []string
	quoteTexts     []string
	reactions      []string
	online         []bool
	subscribed     []string
	typing         []string
	own            []string
	lookups        map[string]string // digits -> WhatsApp ID; a missing number is not on WhatsApp
	phones         map[string]string // LID -> phone JID, as the local LID store holds them
	pictures       map[string]string // chat -> "id|content"; a missing Chat has no picture
	pictureReq     []string          // "chat:knownID" of each ProfilePicture call
	gate           chan struct{}     // while set, Send waits
	groups         map[string][]Participant
	groupErr       error
	groupLookups   int
	groupGate      chan struct{} // while set, GroupParticipants waits
	admins         map[string][]string
	historyAnswer  int // Messages in the answer to RequestHistory; -1 is no answer
	historyReqs    []string
}

// SetOwn sets the user parts of the user's own IDs.
func (f *Fake) SetOwn(users ...string) { f.mu.Lock(); f.own = users; f.mu.Unlock() }

func (f *Fake) OwnUsers() []string { f.mu.Lock(); defer f.mu.Unlock(); return f.own }

// SetGroup presets the Participants that GroupParticipants returns for a Group Chat.
func (f *Fake) SetGroup(chat string, participants ...Participant) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.groups == nil {
		f.groups = map[string][]Participant{}
	}
	f.groups[chat] = participants
}

// SetGroupError makes GroupParticipants fail with err. Pass nil to stop failing.
func (f *Fake) SetGroupError(err error) { f.mu.Lock(); f.groupErr = err; f.mu.Unlock() }

// GroupLookups counts the GroupParticipants calls.
func (f *Fake) GroupLookups() int { f.mu.Lock(); defer f.mu.Unlock(); return f.groupLookups }

func (f *Fake) GroupParticipants(chat string) ([]Participant, error) {
	f.mu.Lock()
	f.groupLookups++
	gate, participants, err := f.groupGate, f.groups[chat], f.groupErr
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return participants, err
}

// SetGroupAdmins presets the users that IsGroupAdmin accepts for each Group Chat. SetGroupError makes it fail too.
func (f *Fake) SetGroupAdmins(admins map[string][]string) {
	f.mu.Lock()
	f.admins = admins
	f.mu.Unlock()
}

func (f *Fake) IsGroupAdmin(chat string, users ...string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(users, func(u string) bool { return slices.Contains(f.admins[chat], u) }), f.groupErr
}

// HoldGroupLookup makes GroupParticipants wait until the test calls it again with false.
func (f *Fake) HoldGroupLookup(hold bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if hold && f.groupGate == nil {
		f.groupGate = make(chan struct{})
	} else if !hold && f.groupGate != nil {
		close(f.groupGate)
		f.groupGate = nil
	}
}

// SetPhones presets the local LID store: LID to phone JID.
func (f *Fake) SetPhones(phones map[string]string) { f.mu.Lock(); f.phones = phones; f.mu.Unlock() }

func (f *Fake) PhoneOf(lid string) string { f.mu.Lock(); defer f.mu.Unlock(); return f.phones[lid] }

// SetLookups presets the answers of Lookup: digits to WhatsApp ID. A number that is not in the map is not on WhatsApp.
func (f *Fake) SetLookups(lookups map[string]string) { f.mu.Lock(); f.lookups = lookups; f.mu.Unlock() }

// Lookup validates like the real port, fails with the send error, then answers from the preset.
func (f *Fake) Lookup(phone string) (string, string, error) {
	digits, err := cleanPhone(phone)
	if err != nil {
		return "", "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return digits, "", f.sendErr
	}
	return digits, f.lookups[digits], nil
}

// SetPictures presets the Profile pictures: chat to "id|content".
func (f *Fake) SetPictures(pictures map[string]string) {
	f.mu.Lock()
	f.pictures = pictures
	f.mu.Unlock()
}

// ProfilePicture fails with the send error, records "chat:knownID", then answers from the preset.
func (f *Fake) ProfilePicture(chat, knownID string) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return "", nil, f.sendErr
	}
	f.pictureReq = append(f.pictureReq, chat+":"+knownID)
	id, content, ok := strings.Cut(f.pictures[chat], "|")
	switch {
	case !ok:
		return "", nil, nil
	case id == knownID:
		return knownID, nil, nil
	}
	return id, []byte(content), nil
}

// PictureRequests lists the ProfilePicture calls that reached WhatsApp, in order.
func (f *Fake) PictureRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.pictureReq...)
}

func NewFake() *Fake { return &Fake{events: make(chan Event, 64), historyAnswer: -1} }

// SetLinked sets the state at start-up.
func (f *Fake) SetLinked(linked bool) { f.mu.Lock(); f.linked = linked; f.mu.Unlock() }

// SetPairCode scripts the answer of PairCode. A non-nil err wins.
func (f *Fake) SetPairCode(code string, err error) {
	f.mu.Lock()
	f.pairCode, f.pairErr = code, err
	f.mu.Unlock()
}

// SetConnectError makes Connect fail with err. Pass nil to stop failing.
func (f *Fake) SetConnectError(err error) { f.mu.Lock(); f.connectErr = err; f.mu.Unlock() }

// SetSendError makes Send fail with err. Pass nil to stop failing.
func (f *Fake) SetPresenceError(err error) { f.mu.Lock(); f.presenceErr = err; f.mu.Unlock() }
func (f *Fake) SetSendError(err error)     { f.mu.Lock(); f.sendErr = err; f.mu.Unlock() }

// Sent lists the IDs that Send accepted, in order.
func (f *Fake) Sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sent...)
}

// SendAttempts counts the Send calls, failed or not.
func (f *Fake) SendAttempts() int { f.mu.Lock(); defer f.mu.Unlock(); return f.attempts }

func (f *Fake) NewMessageID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids++
	return fmt.Sprintf("fake-%d", f.ids)
}

// SentTimers lists the Disappearing timers that Send accepted, in order.
func (f *Fake) SentTimers() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64{}, f.timers...)
}

// SetHold makes Send wait until SetHold(false), so a test can end a send after other events.
func (f *Fake) SetHold(hold bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if hold && f.gate == nil {
		f.gate = make(chan struct{})
	} else if !hold && f.gate != nil {
		close(f.gate)
		f.gate = nil
	}
}

// SetHistoryAnswer presets the answer to RequestHistory: an On-Demand batch of n Messages older than the anchor.
// 0 is an empty batch. The default, -1, is no answer.
func (f *Fake) SetHistoryAnswer(n int) { f.mu.Lock(); f.historyAnswer = n; f.mu.Unlock() }

// HistoryRequests lists the RequestHistory calls as "chat:anchorID:count", in order.
func (f *Fake) HistoryRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.historyReqs...)
}

// RequestHistory fails with the send error, records the call, then plays the preset answer.
func (f *Fake) RequestHistory(chat string, anchor HistoryAnchor, count int) error {
	f.mu.Lock()
	if f.sendErr != nil {
		defer f.mu.Unlock()
		return f.sendErr
	}
	f.historyReqs = append(f.historyReqs, fmt.Sprintf("%s:%s:%d", chat, anchor.ID, count))
	n, call := f.historyAnswer, len(f.historyReqs)
	f.mu.Unlock()
	if n < 0 {
		return nil
	}
	c := HistoryChat{JID: chat, Kind: KindDirect}
	for i := 1; i <= n; i++ {
		c.Messages = append(c.Messages, Message{ID: fmt.Sprintf("older-%d-%d", call, i), Sender: chat, Time: anchor.Time - int64(i), Text: "older"})
	}
	f.Emit(HistorySync{OnDemand: true, Chats: []HistoryChat{c}})
	return nil
}

func (f *Fake) Send(_, id, _ string, quote *Quote, timer int64) error {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, id)
	f.timers = append(f.timers, timer)
	if quote != nil {
		f.quotes = append(f.quotes, id+":"+quote.ID)
		f.quoteTexts = append(f.quoteTexts, quote.Text)
	}
	return nil
}

// SendMedia fails with the send error, like Send. A good call records the Media and its plain content.
func (f *Fake) SendMedia(_, id string, m Media, plain *os.File, caption string, timer int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.sendErr != nil {
		return f.sendErr
	}
	content, err := io.ReadAll(plain)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, id)
	f.timers = append(f.timers, timer)
	f.uploads = append(f.uploads, strings.Join([]string{id, string(m.Kind), m.Mime, m.Name, caption, string(content)}, "|"))
	return nil
}

// Uploads lists the Media that SendMedia accepted as "id|kind|mime|name|caption|content", in order.
func (f *Fake) Uploads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.uploads...)
}

// React fails with the send error, like Send. A good call is recorded as "id:emoji".
func (f *Fake) React(_, id, _, emoji string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.reactions = append(f.reactions, id+":"+emoji)
	return nil
}

// QuoteTexts lists the Quote text of each sent reply, in order.
func (f *Fake) QuoteTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.quoteTexts...)
}

// Quotes lists the sent replies as "id:quotedID", in order. Reactions lists the Reactions as "id:emoji".
func (f *Fake) Quotes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.quotes...)
}

func (f *Fake) Reactions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.reactions...)
}

// SetDownload scripts Download: it writes content to the file, or fails with err (a non-nil err wins).
func (f *Fake) SetDownload(content string, err error) {
	f.mu.Lock()
	f.content, f.dlErr = content, err
	f.mu.Unlock()
}

// SetThumbnail scripts DownloadThumbnail: it returns content, or fails with err (a non-nil err wins).
func (f *Fake) SetThumbnail(content string, err error) {
	f.mu.Lock()
	f.thumb, f.thumbErr = content, err
	f.mu.Unlock()
}

// ThumbnailDownloads counts the DownloadThumbnail calls.
func (f *Fake) ThumbnailDownloads() int { f.mu.Lock(); defer f.mu.Unlock(); return f.thumbDownloads }

// SetThumbnailDelay makes each DownloadThumbnail call take d, so calls overlap.
func (f *Fake) SetThumbnailDelay(d time.Duration) {
	f.mu.Lock()
	f.thumbDelay = d
	f.mu.Unlock()
}

// ThumbnailPeak is the most DownloadThumbnail calls that ran at the same time.
func (f *Fake) ThumbnailPeak() int { f.mu.Lock(); defer f.mu.Unlock(); return f.thumbPeak }

func (f *Fake) DownloadThumbnail(Media) ([]byte, error) {
	f.mu.Lock()
	f.thumbDownloads++
	f.thumbActive++
	f.thumbPeak = max(f.thumbPeak, f.thumbActive)
	delay := f.thumbDelay
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.thumbActive--
	if f.thumbErr != nil {
		return nil, f.thumbErr
	}
	return []byte(f.thumb), nil
}

// Downloads counts the Download calls.
func (f *Fake) Downloads() int { f.mu.Lock(); defer f.mu.Unlock(); return f.downloads }

// SetDownloadDelay makes each Download call take d, so calls overlap.
func (f *Fake) SetDownloadDelay(d time.Duration) {
	f.mu.Lock()
	f.dlDelay = d
	f.mu.Unlock()
}

// DownloadPeak is the most Download calls that ran at the same time.
func (f *Fake) DownloadPeak() int { f.mu.Lock(); defer f.mu.Unlock(); return f.dlPeak }

// SetExpired makes Download fail with ErrMediaExpired for Media with one of these direct paths.
func (f *Fake) SetExpired(paths ...string) { f.mu.Lock(); f.expired = paths; f.mu.Unlock() }

// SetMediaRetryError makes RequestMediaRetry fail with err. Pass nil to stop failing.
func (f *Fake) SetMediaRetryError(err error) { f.mu.Lock(); f.retryErr = err; f.mu.Unlock() }

// MediaRetryRequests lists the RequestMediaRetry calls that worked as "chat|sender|id|fromMe", in order.
func (f *Fake) MediaRetryRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.retries...)
}

func (f *Fake) RequestMediaRetry(chat, sender, id string, fromMe bool, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.retryErr != nil {
		return f.retryErr
	}
	f.retries = append(f.retries, fmt.Sprintf("%s|%s|%s|%t", chat, sender, id, fromMe))
	return nil
}

// Download writes the scripted content through the same cap as the real client.
func (f *Fake) Download(ref Media, dst *os.File, limit int64) error {
	f.mu.Lock()
	f.downloads++
	f.dlActive++
	f.dlPeak = max(f.dlPeak, f.dlActive)
	content, err, delay := f.content, f.dlErr, f.dlDelay
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	f.dlActive--
	expired := slices.Contains(f.expired, ref.DirectPath)
	f.mu.Unlock()
	if expired {
		return fmt.Errorf("%w: %w", ErrMediaExpired, whatsmeow.ErrMediaDownloadFailedWith410)
	}
	if err != nil {
		return err
	}
	_, err = io.WriteString(capDisk{file: dst, max: limit}, content)
	return err
}

// Read is one MarkRead call that worked.
type Read struct {
	Chat   string   `json:"chat"`
	Sender string   `json:"sender"`
	IDs    []string `json:"ids"`
}

// SetReadError makes MarkRead fail with err. Pass nil to stop failing.
func (f *Fake) SetReadError(err error) { f.mu.Lock(); f.readErr = err; f.mu.Unlock() }

// Reads lists the MarkRead calls that worked, in order.
func (f *Fake) Reads() []Read {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Read{}, f.reads...)
}

// Plays lists the MarkPlayed calls that worked, in order. Each has one ID.
func (f *Fake) Plays() []Read {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Read{}, f.plays...)
}

// MarkPlayed fails with the read error, like MarkRead.
func (f *Fake) MarkPlayed(chat, sender, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return f.readErr
	}
	f.plays = append(f.plays, Read{chat, sender, []string{id}})
	return nil
}

// SelfReads lists the MarkReadSelf calls that worked, in order.
func (f *Fake) SelfReads() []Read {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Read{}, f.selfReads...)
}

// MarkReadSelf fails with the read error, like MarkRead.
func (f *Fake) MarkReadSelf(chat, sender string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return f.readErr
	}
	f.selfReads = append(f.selfReads, Read{chat, sender, append([]string{}, ids...)})
	return nil
}

// ChatReads lists the Chats of the MarkChatRead calls, in order.
func (f *Fake) ChatReads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.chatReads...)
}

func (f *Fake) MarkRead(chat, sender string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return f.readErr
	}
	f.reads = append(f.reads, Read{chat, sender, append([]string{}, ids...)})
	return nil
}

func (f *Fake) MarkChatRead(chat string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatReads = append(f.chatReads, chat)
	return nil
}

// SetPinned and SetMuted fail with the send error, like Send. A good call is recorded as "chat:true" and "chat:until".
// #57 records Read Marker writes the same way.
func (f *Fake) SetPinned(chat string, pinned bool) error {
	return f.record(&f.pins, fmt.Sprintf("%s:%t", chat, pinned))
}

func (f *Fake) SetMuted(chat string, until int64) error {
	return f.record(&f.mutes, fmt.Sprintf("%s:%d", chat, until))
}

// SetUnread records "chat:true" or "chat:false". It is not a Read Receipt and not a MarkChatRead.
func (f *Fake) SetUnread(chat string, unread bool) error {
	return f.record(&f.unreads, fmt.Sprintf("%s:%t", chat, unread))
}

// DeleteForMe records "chat:id".
func (f *Fake) DeleteForMe(chat, id, _ string, _ bool, _ int64) error {
	return f.record(&f.deletes, chat+":"+id)
}

// Deletes lists the "delete for me" writes that worked, in order.
func (f *Fake) Deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.deletes...)
}

// Unreads lists the Read Marker writes that worked, in order.
func (f *Fake) Unreads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.unreads...)
}

// record adds an App State write to list, or fails with the send error and records nothing.
func (f *Fake) record(list *[]string, entry string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	*list = append(*list, entry)
	return nil
}

// Pins and Mutes list the App State writes that worked, in order.
func (f *Fake) Pins() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.pins...)
}

func (f *Fake) Mutes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.mutes...)
}

// Edit and Revoke fail with the send error, like Send. A good call is recorded.
func (f *Fake) Edit(_, id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.edits = append(f.edits, id+":"+text)
	return nil
}

func (f *Fake) Revoke(_, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.revokes = append(f.revokes, id)
	return nil
}

// Edits lists the edits that Edit accepted as "id:text", in order. Revokes lists the revoked IDs.
func (f *Fake) Edits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.edits...)
}

func (f *Fake) Revokes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.revokes...)
}

// SetLogoutError makes Logout fail with err. Pass nil to stop failing.
func (f *Fake) SetLogoutError(err error) { f.mu.Lock(); f.logoutErr = err; f.mu.Unlock() }

// Logouts counts the Logout calls.
func (f *Fake) Logouts() int { f.mu.Lock(); defer f.mu.Unlock(); return f.logouts }

func (f *Fake) Logout() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logouts++
	return f.logoutErr
}

// Connects counts the Connect calls.
func (f *Fake) Connects() int { f.mu.Lock(); defer f.mu.Unlock(); return f.connects }

// Emit plays an event. A PairSuccess also links the fake.
func (f *Fake) Emit(e Event) {
	if _, ok := e.(PairSuccess); ok {
		f.SetLinked(true)
	}
	f.events <- e
}

func (f *Fake) Linked() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.linked }

func (f *Fake) Connect() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	return f.connectErr
}

func (f *Fake) PairCode(string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pairCode, f.pairErr
}

func (f *Fake) Disconnect()          {}
func (f *Fake) Events() <-chan Event { return f.events }

// SendPresence, SubscribePresence and SendTyping record the call. Typing is recorded as "chat:composing" or "chat:paused".
func (f *Fake) SendPresence(online bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.presenceErr != nil {
		return f.presenceErr
	}
	f.online = append(f.online, online)
	return nil
}

func (f *Fake) SubscribePresence(jid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = append(f.subscribed, jid)
	return nil
}

func (f *Fake) SendTyping(chat string, composing bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := ActivityPaused
	if composing {
		state = ActivityComposing
	}
	f.typing = append(f.typing, chat+":"+state)
	return nil
}

// OnlineCalls, Subscribed and TypingCalls list what the fake got, in order.
func (f *Fake) OnlineCalls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool{}, f.online...)
}

func (f *Fake) Subscribed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.subscribed...)
}

func (f *Fake) TypingCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.typing...)
}
