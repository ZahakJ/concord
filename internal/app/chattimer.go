package app

import (
	"fmt"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

// chattimer.go: the disappearing-messages timer is a chat setting the core
// keeps, synced between this account's own devices with the pins and archive
// state (chatprefs.go), and the core stamps it on every message sent into the
// chat, whatever sent it: the composer, a GIF, a scheduled send, a reply from
// the notification shade. It used to live in one window's localStorage, so
// any send that did not pass through that window's composer went out bare and
// never disappeared.

// SetChatTimer sets a chat's disappearing timer, in seconds; 0 turns it off.
func (s *Service) SetChatTimer(chatID string, secs int64) error {
	if !store.ValidChatTimer(secs) {
		return fmt.Errorf("app: not a disappearing timer the app offers")
	}
	if !s.haveChannel(chatID) {
		return fmt.Errorf("app: unknown chat")
	}
	all, err := s.store.ChatPrefs()
	if err != nil {
		return err
	}
	cur := all[chatID]
	if cur.Timer == secs && cur.TimerAt > 0 {
		return nil
	}
	return s.commitChatPrefs(map[string]store.ChatPref{
		chatID: {Timer: secs, TimerAt: stampAfter(cur.TimerAt)},
	})
}

// withChatTimer puts the chat's timer on content that does not carry one.
func (s *Service) withChatTimer(channelID, content string) string {
	if store.EphemeralExpiry(content) != 0 {
		return content
	}
	secs := s.store.ChatTimer(channelID)
	if secs == 0 {
		return content
	}
	return fmt.Sprintf("[eph](concord://eph/v1/%d)", time.Now().Unix()+secs) + content
}
