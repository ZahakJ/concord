package app

import (
	"fmt"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/store"
)

// Pinned and archived chats. A "chat" is a direct conversation, 1:1 or group,
// identified by its channel id (the same id read state uses). Pins keep a chat
// at the top of the list in an order the user picked; archiving moves it out of
// the main list. Neither is muting: archived chats still notify by the usual
// notification rules, and a new message never un-archives.
//
// PRIVACY: this is the account's own business. It is held in the encrypted
// store, shown to every UI session of this backend, and propagated to the
// account's OTHER devices the way read markers are (readstate.go): a
// chat_prefs message on the Notes self-group's meta topic, plus the own-device
// hello. It is never published to any chat, guild, rendezvous or relay, and the
// hello field is only offered to a proven device of this account.
//
// Conflicts: each of the two facts (pinned+order, archived) is last-writer-wins
// on its own stamp (UnixMilli), stamps capped at readSkew-like bound
// store.ChatPrefSkew ahead of the local clock, ties broken on content so every
// device lands on the same answer. Cleared facts keep their row as a
// tombstone, so a late older change cannot resurrect them.

// MaxPinnedChats is the pin cap.
const MaxPinnedChats = 10

// helloChatPrefs bounds how many records one hello carries (newest stamps).
const helloChatPrefs = 256

// OnChatPrefs registers a callback fired whenever pins or archive state change,
// locally or from another device.
func (s *Service) OnChatPrefs(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChatPrefs = append(s.onChatPrefs, fn)
}

func (s *Service) emitChatPrefs() {
	s.mu.RLock()
	cbs := append([]func(){}, s.onChatPrefs...)
	s.mu.RUnlock()
	for _, fn := range cbs {
		fn()
	}
}

// ChatPrefs returns every chat's pin/archive record.
func (s *Service) ChatPrefs() (map[string]store.ChatPref, error) {
	return s.store.ChatPrefs()
}

// isDirectChat: a known, non-Notes direct conversation's channel.
func (s *Service) isDirectChat(chatID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	gid, ok := s.channelToGuild[chatID]
	if !ok {
		return false
	}
	g, ok := s.guilds[gid]
	return ok && g.Kind == "dm" && !isNotesGuild(*g, s.PublicKey())
}

// stampAfter is a stamp that beats an existing one even if our clock is behind.
func stampAfter(prev int64) int64 {
	at := time.Now().UnixMilli()
	if at <= prev {
		at = prev + 1
	}
	return at
}

// SetChatPinned pins (at the bottom of the pins) or unpins a chat.
func (s *Service) SetChatPinned(chatID string, pinned bool) error {
	if !s.isDirectChat(chatID) {
		return fmt.Errorf("app: only direct chats can be pinned")
	}
	all, err := s.store.ChatPrefs()
	if err != nil {
		return err
	}
	cur := all[chatID]
	if cur.Pinned == pinned && cur.PinAt > 0 {
		return nil
	}
	rec := store.ChatPref{Pinned: pinned, PinAt: stampAfter(cur.PinAt)}
	if pinned {
		n, top := 0, 0.0
		for _, r := range all {
			if r.Pinned {
				n++
				if r.PinKey > top {
					top = r.PinKey
				}
			}
		}
		if n >= MaxPinnedChats {
			return fmt.Errorf("app: you can pin up to %d chats; unpin one first", MaxPinnedChats)
		}
		rec.PinKey = top + 1
	}
	return s.commitChatPrefs(map[string]store.ChatPref{chatID: rec})
}

// SetChatPinOrder puts the given pinned chats in that order (first = top).
// Ids that are not pinned are ignored.
func (s *Service) SetChatPinOrder(ordered []string) error {
	all, err := s.store.ChatPrefs()
	if err != nil {
		return err
	}
	changes := map[string]store.ChatPref{}
	key := 0.0
	for _, id := range ordered {
		cur, ok := all[id]
		if !ok || !cur.Pinned {
			continue
		}
		key++
		if cur.PinKey != key {
			changes[id] = store.ChatPref{Pinned: true, PinKey: key, PinAt: stampAfter(cur.PinAt)}
		}
	}
	return s.commitChatPrefs(changes)
}

// SetChatArchived archives or unarchives a chat.
func (s *Service) SetChatArchived(chatID string, archived bool) error {
	if !s.isDirectChat(chatID) {
		return fmt.Errorf("app: only direct chats can be archived")
	}
	all, err := s.store.ChatPrefs()
	if err != nil {
		return err
	}
	cur := all[chatID]
	if cur.Archived == archived && cur.ArchAt > 0 {
		return nil
	}
	return s.commitChatPrefs(map[string]store.ChatPref{
		chatID: {Archived: archived, ArchAt: stampAfter(cur.ArchAt)},
	})
}

// commitChatPrefs merges local changes, then tells the UIs and our own devices.
func (s *Service) commitChatPrefs(changes map[string]store.ChatPref) error {
	sent := map[string]store.ChatPref{}
	for id, rec := range changes {
		got, changed, err := s.store.MergeChatPref(id, rec, time.Now())
		if err != nil {
			return err
		}
		if changed {
			sent[id] = got
		}
	}
	if len(sent) == 0 {
		return nil
	}
	s.emitChatPrefs()
	s.broadcastChatPrefs(sent)
	return nil
}

// applyRemoteChatPrefs folds in records authored by another of our devices
// (authenticated by the caller). It never re-broadcasts.
func (s *Service) applyRemoteChatPrefs(recs map[string]store.ChatPref) {
	changed := false
	for id, rec := range recs {
		if id == "" {
			continue
		}
		if _, ch, err := s.store.MergeChatPref(id, rec, time.Now()); err == nil && ch {
			changed = true
		}
	}
	if changed {
		s.emitChatPrefs()
	}
}

// haveChannel reports whether a channel of any guild this device holds has id.
func (s *Service) haveChannel(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.guilds {
		for _, c := range g.Channels {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}

// dropChatPrefs forgets records for chats that no longer exist.
func (s *Service) dropChatPrefs(chatIDs []string) {
	if len(chatIDs) == 0 {
		return
	}
	if err := s.store.DeleteChatPrefs(chatIDs); err == nil {
		s.emitChatPrefs()
	}
}

// broadcastChatPrefs publishes to the Notes self-group (own devices only) and
// hands the records to connected own devices over the hello stream. A
// single-device account has no Notes meta peers and pays nothing.
func (s *Service) broadcastChatPrefs(recs map[string]store.ChatPref) {
	notes, ok := s.findNotesDM()
	if !ok {
		return
	}
	s.publishMeta(notes.GroupID, guildMeta{Type: "chat_prefs", ChatPrefs: recs})
	for _, p := range s.host.Peers() {
		if !s.ownDevice(p) || s.revocationFor(p) != nil {
			continue
		}
		go s.sayOwnFrame(p, helloFrame{Credential: s.myCredential, ChatPrefs: recs})
	}
}

// chatPrefsFor is what a hello offers peer p: nil for anyone who is not a
// current device of this account.
func (s *Service) chatPrefsFor(p peer.ID) map[string]store.ChatPref {
	if !s.ownDevice(p) || s.revocationFor(p) != nil {
		return nil
	}
	all, err := s.store.ChatPrefs()
	if err != nil || len(all) == 0 {
		return nil
	}
	return newestChatPrefs(all, helloChatPrefs)
}

func (s *Service) adoptOfferedChatPrefs(from peer.ID, recs map[string]store.ChatPref) {
	if len(recs) == 0 || !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	s.applyRemoteChatPrefs(recs)
}

// newestChatPrefs keeps the n records with the newest stamps.
func newestChatPrefs(m map[string]store.ChatPref, n int) map[string]store.ChatPref {
	if len(m) <= n {
		return m
	}
	stamp := func(r store.ChatPref) int64 {
		if r.PinAt > r.ArchAt {
			return r.PinAt
		}
		return r.ArchAt
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return stamp(m[ids[i]]) > stamp(m[ids[j]]) })
	out := make(map[string]store.ChatPref, n)
	for _, id := range ids[:n] {
		out[id] = m[id]
	}
	return out
}
