package contract

import "time"

// maxMuteWait is the longest wait of the Mute end timer, in seconds (24 h).
const maxMuteWait = 24 * 60 * 60

// scheduleMuteEnd sets one timer for the earliest Mute end, so the Plugin counts the Chat again when its Mute ends.
// It keeps the timer that waits for the same end, and drops an older one.
func (s *session) scheduleMuteEnd() {
	until, ok, err := s.chats.NextMuteEnd()
	switch {
	case storeFailed(s.logOut, storeRead, err):
	case !ok:
		s.muteTick, s.muteAt = nil, 0
	case until != s.muteAt:
		// The wait is capped: a far end must not overflow the Duration. The tick re-checks the end.
		wait := time.Duration(min(until-Clock.Now().Unix(), maxMuteWait)) * time.Second
		s.muteTick, s.muteAt = Clock.After(wait), until
	}
}
