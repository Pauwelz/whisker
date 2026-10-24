package whatsapp

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Text lines for Messages that whisker cannot show in full. The Plugin shows them as plain text.
const (
	openOnPhone     = "Open it on your phone."
	unsupportedText = "Unsupported message. " + openOnPhone
	viewOnceText    = "View-once media. " + openOnPhone
)

// content is what the Plugin shows for one Message: text, Media and the JIDs that the text mentions.
type content struct {
	Text     string
	Media    *Media
	Mentions []string
}

// contentOf maps a Message to its content. It reports false for a Message with nothing to show:
// a Reaction, a poll vote, a protocol Message. Every other Message shows as something, never as a blank line.
func contentOf(m *waE2E.Message, viewOnce bool) (content, bool) {
	if viewOnce || m.GetImageMessage().GetViewOnce() || m.GetVideoMessage().GetViewOnce() || m.GetAudioMessage().GetViewOnce() {
		return content{Text: viewOnceText}, true // the Media is not kept: only the phone opens it
	}
	if text, media := messageText(m), mediaOf(m); text != "" || media != nil {
		return content{Text: text, Media: media, Mentions: mentionsOf(m)}, true
	}
	if text := placeholderOf(m); text != "" {
		return content{Text: text}, true
	}
	if hasVisibleContent(m) {
		return content{Text: unsupportedText}, true
	}
	return content{}, false
}

// placeholderOf is the one-line text of a poll, an event, a contact card or a location. Empty for other Messages.
func placeholderOf(m *waE2E.Message) string {
	for _, poll := range []*waE2E.PollCreationMessage{m.GetPollCreationMessage(), m.GetPollCreationMessageV2(), m.GetPollCreationMessageV3(), m.GetPollCreationMessageV5(), m.GetPollCreationMessageV6()} {
		if poll != nil {
			return fmt.Sprintf("Poll: %s. %s", poll.GetName(), openOnPhone)
		}
	}
	if m.GetPollCreationMessageV4() != nil {
		return "Poll. " + openOnPhone
	}
	if ev := m.GetEventMessage(); ev != nil {
		return fmt.Sprintf("Event: %s. %s", ev.GetName(), openOnPhone)
	}
	var cards []*waE2E.ContactMessage
	if c := m.GetContactMessage(); c != nil {
		cards = append(cards, c)
	}
	cards = append(cards, m.GetContactsArrayMessage().GetContacts()...)
	if len(cards) > 0 {
		lines := make([]string, len(cards))
		for i, c := range cards {
			lines[i] = contactLine(c.GetDisplayName(), c.GetVcard())
		}
		return strings.Join(lines, "\n")
	}
	if loc := m.GetLocationMessage(); loc != nil {
		return locationLine(loc.GetName(), loc.GetAddress(), loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
	}
	if loc := m.GetLiveLocationMessage(); loc != nil {
		return locationLine("", loc.GetCaption(), loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
	}
	return ""
}

// contactLine is "Contact: name, number". The number is the first TEL line of the vCard.
func contactLine(name, vcard string) string {
	line := "Contact: " + name
	for _, l := range strings.Split(vcard, "\n") {
		if strings.HasPrefix(l, "TEL") {
			if _, number, ok := strings.Cut(l, ":"); ok {
				return line + ", " + strings.TrimSpace(number)
			}
		}
	}
	return line
}

// locationLine is the place name, then a map link on its own line. The Plugin makes the link clickable.
func locationLine(name, address string, lat, lon float64) string {
	title := "Location"
	if label := strings.TrimSpace(name + " " + address); label != "" {
		title += ": " + label
	}
	return fmt.Sprintf("%s\nhttps://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=17/%.6f/%.6f", title, lat, lon, lat, lon)
}

// mentionsOf returns the JIDs that a Message mentions. Text and captions carry them in a ContextInfo.
func mentionsOf(m *waE2E.Message) []string {
	for _, ctx := range []interface{ GetMentionedJID() []string }{
		m.GetExtendedTextMessage().GetContextInfo(), m.GetImageMessage().GetContextInfo(),
		m.GetVideoMessage().GetContextInfo(), m.GetDocumentMessage().GetContextInfo(),
	} {
		if jids := ctx.GetMentionedJID(); len(jids) > 0 {
			return jids
		}
	}
	return nil
}

// mentionsUser reports whether a JID in mentions has one of the user parts (the phone number and the LID of the user).
func mentionsUser(mentions []string, users ...string) bool {
	for _, jid := range mentions {
		user, _, _ := strings.Cut(jid, "@")
		user, _, _ = strings.Cut(user, ":")
		for _, u := range users {
			if u != "" && u == user {
				return true
			}
		}
	}
	return false
}

// hasVisibleContent reports whether a Message holds something besides metadata and the kinds that have their own handling.
func hasVisibleContent(m *waE2E.Message) bool {
	visible := false
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		switch fd.Name() {
		case "messageContextInfo", "senderKeyDistributionMessage", "protocolMessage", "reactionMessage",
			"encReactionMessage", "pollUpdateMessage", "encEventResponseMessage", "keepInChatMessage":
			return true
		}
		visible = true
		return false
	})
	return visible
}
