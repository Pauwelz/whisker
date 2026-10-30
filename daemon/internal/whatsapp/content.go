package whatsapp

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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
// a Reaction, a poll vote, a protocol Message, an album header. Every other Message shows as something, never as a blank line.
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

// contextOf is the ContextInfo of the content field that a Message sets, or nil. It reads every kind, also kinds that WhatsApp adds later and a kind inside a FutureProofMessage.
// The top-level messageContextInfo is another type and holds no timer or mention, so it is skipped.
func contextOf(m *waE2E.Message) *waE2E.ContextInfo {
	var info *waE2E.ContextInfo
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() || fd.Name() == "messageContextInfo" {
			return true
		}
		if fp, ok := v.Message().Interface().(*waE2E.FutureProofMessage); ok {
			info = contextOf(fp.GetMessage())
			return info == nil
		}
		field := v.Message().Descriptor().Fields().ByName("contextInfo")
		if field == nil || !v.Message().Has(field) {
			return true
		}
		info, _ = v.Message().Get(field).Message().Interface().(*waE2E.ContextInfo)
		return false
	})
	return info
}

// mentionsOf returns the JIDs that a Message mentions. Text and captions carry them in a ContextInfo.
func mentionsOf(m *waE2E.Message) []string {
	return contextOf(m).GetMentionedJID()
}

// MaxMentions is the most mentions that the Daemon keeps for one Message. A group holds about 1024 people.
const MaxMentions = 256

// mentionWord returns the user part that follows the "@" at text[at], and where it ends.
// A user part ends at the first rune that is not a letter or a digit, so "@12" never matches "@123".
func mentionWord(text string, at int) (word string, end int) {
	end = at + 1
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		end += size
	}
	return text[at+1 : end], end
}

// UniqueMentions drops the repeated JIDs and keeps the first MaxMentions. The input is not changed.
func UniqueMentions(jids []string) []string {
	seen := make(map[string]bool, min(len(jids), MaxMentions))
	out := make([]string, 0, min(len(jids), MaxMentions))
	for _, jid := range jids {
		if len(out) == MaxMentions {
			break
		}
		if !seen[jid] {
			seen[jid] = true
			out = append(out, jid)
		}
	}
	return out
}

// MentionsIn keeps the JIDs whose "@<user part>" token is in the text. It also applies UniqueMentions.
func MentionsIn(text string, jids []string) []string {
	if len(jids) == 0 {
		return nil
	}
	words := map[string]bool{}
	for i := 0; i < len(text); i++ {
		if text[i] == '@' {
			word, end := mentionWord(text, i)
			words[word] = true
			i = end - 1
		}
	}
	present := make([]string, 0, min(len(jids), MaxMentions))
	for _, jid := range jids {
		if words[userOf(jid)] {
			present = append(present, jid)
		}
	}
	return UniqueMentions(present)
}

// ReplaceMentions replaces each "@<user part>" token with its label, in one left-to-right pass.
// A label is never scanned again.
func ReplaceMentions(text string, labels map[string]string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		word, end := mentionWord(text, i)
		if label, ok := labels[word]; ok {
			b.WriteString(text[last:i])
			b.WriteString(label)
			last = end
		}
		i = end - 1
	}
	b.WriteString(text[last:])
	return b.String()
}

// mentionsUser reports whether a JID in mentions has one of the user parts (the phone number and the LID of the user).
func mentionsUser(mentions []string, users ...string) bool {
	for _, jid := range mentions {
		user, _, _ := strings.Cut(userOf(jid), ":")
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
			"encReactionMessage", "pollUpdateMessage", "encEventResponseMessage", "keepInChatMessage", "secretEncryptedMessage",
			"albumMessage": // the album header holds only a count: its Media come as their own Messages

			return true
		}
		visible = true
		return false
	})
	return visible
}
