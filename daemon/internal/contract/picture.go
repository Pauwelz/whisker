package contract

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
)

// pictureMaxAge is how long a cached Profile picture counts as current without a call to WhatsApp.
const pictureMaxAge = 24 * time.Hour

// chatPicture returns the small Profile picture of a Chat as base64 JPEG, or "" when it has none or WhatsApp hides it.
// A cache younger than pictureMaxAge answers alone. Offline, and after a failed call, the cache answers however old it is.
func (s *session) chatPicture(req *request) outcome {
	chat := req.Params.Chat
	if _, refusal := s.knownChat(req, chat); refusal != nil {
		return outcome{reply: refusal}
	}
	cached, ok, err := s.chats.Picture(chat)
	if err != nil {
		return outcome{reply: s.storeFailed(req)}
	}
	if !ok || Clock.Now().Unix()-cached.FetchedAt >= int64(pictureMaxAge/time.Second) {
		cached, err = s.refreshPicture(chat, cached)
		if err != nil {
			return outcome{reply: s.storeFailed(req)}
		}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"chat": chat, "data": base64.StdEncoding.EncodeToString(cached.Data)})}
}

// refreshPicture asks WhatsApp for a newer picture and caches the answer. It returns the cache when WhatsApp gives no answer.
func (s *session) refreshPicture(chat string, cached chatstore.Picture) (chatstore.Picture, error) {
	// shortcut: ProfilePicture blocks the Daemon loop (up to 15 s), upgrade when another request must run meanwhile.
	id, data, err := s.port.ProfilePicture(chat, cached.ID)
	if err != nil {
		fmt.Fprintln(s.logOut, "picture fetch failed") // no error text: it could hold names
		return cached, nil
	}
	if data == nil && id != "" && id == cached.ID { // unchanged
		return cached, s.chats.TouchPicture(chat)
	}
	if err := s.chats.PutPicture(chat, id, data); err != nil {
		return cached, err
	}
	return chatstore.Picture{ID: id, Data: data}, nil
}

// pictureChanged drops the cached picture of a Chat and tells the Plugin to ask again.
func (s *session) pictureChanged(chat string) bool {
	if err := s.chats.DeletePicture(chat); storeFailed(s.logOut, storeWrite, err) {
		return true
	}
	return s.send(notification("pictureChanged", map[string]any{"chat": chat}))
}
