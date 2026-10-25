package whatsapp

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// SystemID is the ID of a System Message: the same change gives the same ID live and in History Sync,
// so the (chat, id) key of the chat store keeps one copy. actor is "" for the user, time is Unix seconds.
func SystemID(chat, actor string, s System, time int64) string {
	targets := slices.Sorted(slices.Values(s.Targets))
	sum := sha256.Sum256([]byte(strings.Join([]string{chat, s.Kind, actor, strings.Join(targets, ","), s.Value, strconv.FormatInt(time, 10)}, "\x00")))
	return "sys-" + hex.EncodeToString(sum[:12])
}

// systemMessage builds a System Message of a Chat. actor is who made the change (empty when unknown), alt its phone JID if known.
// The actor and the targets become canonical phone JIDs, and the user becomes FromMe and the "" target, so SystemID matches across sources.
func (c *Client) systemMessage(chat, actor, alt types.JID, kind string, targets []types.JID, value string, at time.Time) Message {
	m := Message{Time: at.Unix(), System: &System{Kind: kind, Value: value}}
	if !actor.IsEmpty() {
		sender := c.phone(actor, alt)
		m.Sender, m.FromMe = sender.String(), c.isOwn(sender)
	}
	for _, t := range targets {
		p := c.phone(t)
		if c.isOwn(p) {
			m.System.Targets = append(m.System.Targets, "")
		} else {
			m.System.Targets = append(m.System.Targets, p.String())
		}
	}
	hashActor := m.Sender
	if m.FromMe {
		hashActor = ""
	}
	m.ID = SystemID(chat.String(), hashActor, *m.System, m.Time)
	return m
}

func deref(j *types.JID) types.JID {
	if j == nil {
		return types.EmptyJID
	}
	return *j
}

// groupInfoMessages splits a live group change into one System Message per kind, in a fixed order.
// Other changes (invite links, locks, announce mode and so on) give none.
func (c *Client) groupInfoMessages(evt *events.GroupInfo) []Message {
	actor, alt := deref(evt.Sender), deref(evt.SenderPN)
	sys := func(kind string, targets []types.JID, value string) Message {
		return c.systemMessage(evt.JID, actor, alt, kind, targets, value, evt.Timestamp)
	}
	var out []Message
	if len(evt.Join) > 0 {
		kind := SystemAdd
		if evt.JoinReason == "invite" {
			kind = SystemInvite
		}
		out = append(out, sys(kind, evt.Join, ""))
	}
	// A Participant who is the actor left; the others were removed.
	var left, removed []types.JID
	for _, t := range evt.Leave {
		if !actor.IsEmpty() && c.phone(t) == c.phone(actor, alt) {
			left = append(left, t)
		} else {
			removed = append(removed, t)
		}
	}
	if len(left) > 0 {
		out = append(out, sys(SystemLeave, left, ""))
	}
	if len(removed) > 0 {
		out = append(out, sys(SystemRemove, removed, ""))
	}
	if len(evt.Promote) > 0 {
		out = append(out, sys(SystemPromote, evt.Promote, ""))
	}
	if len(evt.Demote) > 0 {
		out = append(out, sys(SystemDemote, evt.Demote, ""))
	}
	if evt.Name != nil {
		out = append(out, sys(SystemSubject, nil, evt.Name.Name))
	}
	if evt.Topic != nil {
		out = append(out, sys(SystemDescription, nil, deletedIf(evt.Topic.TopicDeleted)))
	}
	if evt.Ephemeral != nil {
		out = append(out, sys(SystemTimer, nil, strconv.FormatUint(uint64(groupTimer(evt.Ephemeral)), 10)))
	}
	return out
}

// groupTimer is the Disappearing timer in seconds of a group change, 0 when off.
func groupTimer(e *types.GroupEphemeral) uint32 {
	if !e.IsEphemeral {
		return 0
	}
	return e.DisappearingTimer
}

// joinedGroupMessage is the System Message of a Group Chat that the user joined: created, added, or joined by link.
// shortcut: whatsmeow gives no time for an add or a join by link, so it is now and a later History Sync copy shows twice;
// upgrade if whatsmeow exposes the notification time.
func (c *Client) joinedGroupMessage(evt *events.JoinedGroup) Message {
	actor, alt := deref(evt.Sender), deref(evt.SenderPN)
	own := c.cli.Store.GetJID().ToNonAD()
	switch {
	case evt.Type == "new":
		return c.systemMessage(evt.JID, actor, alt, SystemCreate, nil, "", evt.GroupCreated)
	case evt.Reason == "invite":
		return c.systemMessage(evt.JID, types.EmptyJID, types.EmptyJID, SystemInvite, []types.JID{own}, "", time.Now())
	}
	return c.systemMessage(evt.JID, actor, alt, SystemAdd, []types.JID{own}, "", time.Now())
}

func deletedIf(deleted bool) string {
	if deleted {
		return SystemDeleted
	}
	return ""
}

// stubKinds maps the History Sync stubs that whisker shows to System kinds. Every other stub is dropped.
var stubKinds = map[waWeb.WebMessageInfo_StubType]string{
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD:     SystemAdd,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE:  SystemInvite,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE:   SystemLeave,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE:  SystemRemove,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE: SystemPromote,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE:  SystemDemote,
	waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT:      SystemSubject,
	waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION:  SystemDescription,
	waWeb.WebMessageInfo_GROUP_CHANGE_ICON:         SystemIcon,
	waWeb.WebMessageInfo_GROUP_CREATE:              SystemCreate,
	waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING:  SystemTimer,
}

// stubMessage maps a History Sync stub to a System Message. It reports false for a stub that whisker drops.
// The parameters are not documented by whatsmeow: Participant JIDs for Participant stubs, the subject,
// the description (none when deleted), the timer in seconds.
// shortcut: an icon stub counts as removed when its parameter is "remove", upgrade when the manual check shows the real format.
func (c *Client) stubMessage(chat types.JID, wm *waWeb.WebMessageInfo) (Message, bool) {
	kind, ok := stubKinds[wm.GetMessageStubType()]
	if !ok || (kind != SystemTimer && chat.Server != types.GroupServer) {
		return Message{}, false
	}
	params := wm.GetMessageStubParameters()
	first := ""
	if len(params) > 0 {
		first = params[0]
	}
	var actor types.JID
	if wm.GetKey().GetFromMe() {
		actor = c.cli.Store.GetJID().ToNonAD()
	} else if s := cmp.Or(wm.GetParticipant(), wm.GetKey().GetParticipant()); s != "" {
		actor, _ = types.ParseJID(s)
	} else if chat.Server != types.GroupServer {
		actor = chat // the other person of a Direct Chat
	}
	var targets []types.JID
	value := ""
	switch kind {
	case SystemSubject:
		value = first
	case SystemDescription:
		value = deletedIf(first == "")
	case SystemIcon:
		value = deletedIf(first == "remove")
	case SystemTimer:
		value = cmp.Or(first, "0")
	case SystemCreate:
	default:
		for _, p := range params {
			if jid, err := types.ParseJID(p); err == nil && jid.User != "" {
				targets = append(targets, jid)
			}
		}
	}
	return c.systemMessage(chat, actor, types.EmptyJID, kind, targets, value, time.Unix(int64(wm.GetMessageTimestamp()), 0)), true
}
