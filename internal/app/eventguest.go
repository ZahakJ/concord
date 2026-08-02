package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zahak/concord/internal/domain"
)

// Guest-opened calendar events: "here's the event, here's a link anyone can
// join with". Opening an event to guests mints a DISPOSABLE MEETING GUILD on
// THIS node — the same knock-to-enter machinery browser guests and the public
// booking page already ride — whose guest link goes into the event record
// itself, so every member's card can offer Copy-link and the ICS export
// carries it into whatever calendar the invitee uses.
//
// This is the booking flow inverted: booking lets a stranger pick a slot and
// the room appears; here the HOST picks the event and shares the room. The
// room is a STANDALONE meeting guild referenced BY the event, not a child of
// the host guild, for the same reason bookings write to Notes instead of a
// new store: a meeting guild already has the whole lifecycle a disposable
// room needs (guest links, expiry sweep on every device, the relayed guest
// session with chat + voice/video), while the host guild's MLS group must
// never admit an unauthenticated stranger — guests talk to the MEETING's
// group through the host's relay, and full members keep E2EE among
// themselves.
//
// The record below is deliberately LOCAL to the minting node. The guest link
// only works while this node answers (it is the guest's crypto endpoint), the
// tokens live in this node's encrypted store, and the knock/auto-admit choice
// is enforced in THIS node's serveGuest — so "who hosts the room" has exactly
// one answer and revocation is never a distributed-consensus problem. What
// other members need — the URL, and who to ask about it — travels on the
// event itself (GuestURL/GuestHost), gated on receive so only the recorded
// host's frames can touch it.

const (
	// eventGuestsKey persists this node's event→room map in the same encrypted
	// store as the guest tokens it complements.
	eventGuestsKey = "events.guest"

	// eventGuestKeepOpen is how long a room stays joinable once opened. A meeting
	// room is NOT a countdown that dies at the stroke of the scheduled end —
	// people join late, meetings run over, "let's continue tomorrow" is normal,
	// and Teams keeps the room around for exactly this. So the room lives until
	// the host ENDS it (or deletes the event), with this as the automatic
	// backstop that reclaims a forgotten room. Re-opening refreshes it. Kept just
	// under maxMeetingLifetime so opening never trips the too-far-ahead guard.
	eventGuestKeepOpen = 30 * 24 * time.Hour
	// eventGuestDefaultLen stands in for a missing end time: an open-ended
	// event holds its room for two hours, matching the "sane default" the UI's
	// happening-now logic assumes (one hour) with room to spare.
	eventGuestDefaultLen = 2 * time.Hour

	// maxEventGuestRoomName bounds the room name seeded from the title — a
	// 120-rune event title is fine on a card, unwieldy in a sidebar.
	maxEventGuestRoomName = 40
	// maxEventGuestSeedRunes bounds how much of the event's details the opening
	// message repeats. Guests get context, not the whole essay — the details
	// live on the card, and the seed rides every guest's history frame.
	maxEventGuestSeedRunes = 500
)

// eventGuestRecord ties one event to the meeting room this node hosts for it.
type eventGuestRecord struct {
	EventID        string `json:"eventId"`
	GuildID        string `json:"guildId"`        // the guild whose calendar owns the event
	MeetingGuildID string `json:"meetingGuildId"` // the disposable guest room
	// AutoAdmit is the host's door choice: false (the default) makes every
	// arriving guest KNOCK — the link may travel anywhere, so arrival is the
	// host's decision, exactly as booking rooms behave. True is for an openly
	// shared event: guests walk straight in while the door is unlocked.
	AutoAdmit   bool  `json:"autoAdmit"`
	ExpiresUnix int64 `json:"expiresUnix"`
}

// initEventGuests restores the event→room map, pruning records whose room or
// event no longer exists (the startup meeting sweep has already run).
func (s *Service) initEventGuests() {
	loaded := map[string]eventGuestRecord{}
	if raw, err := s.store.GetSetting(eventGuestsKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &loaded)
	}
	s.mu.RLock()
	for id, rec := range loaded {
		if _, ok := s.guilds[rec.MeetingGuildID]; !ok {
			delete(loaded, id)
		}
	}
	s.mu.RUnlock()
	for id, rec := range loaded {
		if _, found, err := s.store.EventByID(rec.EventID); err != nil || !found {
			delete(loaded, id)
		}
	}
	s.eventGuestMu.Lock()
	s.eventGuests = loaded
	s.eventGuestMu.Unlock()
	s.saveEventGuests()
}

func (s *Service) saveEventGuests() {
	s.eventGuestMu.Lock()
	out := make(map[string]eventGuestRecord, len(s.eventGuests))
	for id, rec := range s.eventGuests {
		out[id] = rec
	}
	s.eventGuestMu.Unlock()
	if blob, err := json.Marshal(out); err == nil {
		_ = s.store.SetSetting(eventGuestsKey, string(blob))
	}
}

// eventGuestKnocks reports whether a guild is an event room whose guests must
// knock. Consulted by serveGuest beside isBookingMeeting: the door policy is
// enforced where the guests actually arrive, on this node, never trusted from
// anything synced.
func (s *Service) eventGuestKnocks(guildID string) bool {
	s.eventGuestMu.Lock()
	defer s.eventGuestMu.Unlock()
	for _, rec := range s.eventGuests {
		if rec.MeetingGuildID == guildID {
			return !rec.AutoAdmit
		}
	}
	return false
}

// eventGuestRoomSpan derives the room's lifetime from the event's times: the
// stated end (or start + a default length when open-ended); the room's EXPIRY is
// anchored to now, not the end — a room outlives its scheduled slot on purpose.
func eventGuestRoomSpan(ev domain.Event) (end, expiry time.Time) {
	end = time.Unix(ev.StartUnix, 0).Add(eventGuestDefaultLen)
	if ev.EndUnix > 0 {
		end = time.Unix(ev.EndUnix, 0)
	}
	return end, time.Now().Add(eventGuestKeepOpen)
}

// eventGuestRoomName seeds the room's sidebar name from the event title.
func eventGuestRoomName(title string) string {
	if utf8.RuneCountInString(title) > maxEventGuestRoomName {
		runes := []rune(title)
		title = strings.TrimSpace(string(runes[:maxEventGuestRoomName])) + "…"
	}
	return "📅 " + title
}

// OpenEventGuests mints (or returns) the guest room for an event, permission-
// gated exactly like editing it. Called again for an already-open event it
// returns the SAME link — a link the host pasted into invitations must never
// silently rotate — updating only the door choice.
func (s *Service) OpenEventGuests(guildID, eventID string, autoAdmit bool) (domain.Event, error) {
	groupID, ok := s.eventGroup(guildID)
	if !ok {
		return domain.Event{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	ev, found, err := s.store.EventByID(eventID)
	if err != nil {
		return domain.Event{}, err
	}
	if !found || ev.GuildID != guildID {
		return domain.Event{}, fmt.Errorf("app: unknown event %s", eventID)
	}
	if !s.mayManageEvent(guildID, s.id.Fingerprint(), ev) {
		return domain.Event{}, fmt.Errorf("app: only the event's author or a moderator can invite guests")
	}
	// Nesting rooms inside rooms helps nobody, and a DM's "event" is already
	// private to people who can just be invited properly.
	s.mu.RLock()
	hostKind := ""
	if g, ok := s.guilds[guildID]; ok {
		hostKind = g.Kind
	}
	s.mu.RUnlock()
	if hostKind == "meeting" {
		return domain.Event{}, fmt.Errorf("app: a meeting already has its own guest link")
	}
	if s.guestGatewayBase() == "" {
		return domain.Event{}, fmt.Errorf("app: guest links need a rendezvous server (Settings → Connection)")
	}

	_, expiry := eventGuestRoomSpan(ev)
	now := time.Now()
	// The room's expiry is absolute, and every device refuses meetings pinned
	// further out than the longest lifetime on the menu — so an event a season
	// away opens its doors closer to the date, like a booking horizon.
	if expiry.After(now.Add(maxMeetingLifetime)) {
		return domain.Event{}, fmt.Errorf("app: too far ahead — open guest access within %d days of the event", int(maxMeetingLifetime.Hours()/24))
	}

	// Already open here: same link, new door choice, refreshed expiry.
	s.eventGuestMu.Lock()
	rec, exists := s.eventGuests[eventID]
	if exists {
		rec.AutoAdmit = autoAdmit
		rec.ExpiresUnix = expiry.Unix()
		s.eventGuests[eventID] = rec
	}
	s.eventGuestMu.Unlock()
	if exists {
		s.saveEventGuests()
		s.setMeetingExpiry(rec.MeetingGuildID, expiry)
		// CreateGuestLink reuses the existing token and stamps it with the
		// meeting's (just refreshed) absolute expiry. The member code is
		// re-minted too: it carries our CURRENT addresses (fresher is more
		// dialable), and an event opened before member Join existed picks the
		// code up here instead of needing a new room.
		url, uerr := s.CreateGuestLink(rec.MeetingGuildID, 0)
		code, cerr := s.InviteCode(rec.MeetingGuildID)
		if uerr == nil && cerr == nil && (url != ev.GuestURL || code != ev.MemberCode) {
			return s.stampEventGuest(groupID, ev, url, code)
		}
		return ev, nil
	}
	if ev.GuestURL != "" {
		// Another device of another account already hosts a room for this event;
		// a second room would hand out two competing links.
		return domain.Event{}, fmt.Errorf("app: this event already has a guest link (hosted by another member)")
	}

	// Mint the room: the same disposable machinery bookings use.
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Event{}, fmt.Errorf("app: couldn't create the guest room — try again")
	}
	room := domain.NewGuild(eventGuestRoomName(ev.Title), gid, s.PublicKey())
	room.Kind = "meeting"
	room.Channels[0].Name = "meeting"
	if err := s.store.SaveGuild(room); err != nil {
		return domain.Event{}, fmt.Errorf("app: couldn't create the guest room — try again")
	}
	s.trackGuild(&room)
	s.setMeetingExpiry(room.ID, expiry)

	url, err := s.CreateGuestLink(room.ID, 0) // 0: inherit the absolute expiry above
	if err != nil {
		_ = s.deleteGuildLocal(room.ID)
		s.dropGuestTokens(room.ID)
		return domain.Event{}, err
	}
	// The members' door: a real invite into the room, so anyone already
	// trusted with the event (guild/DM members — the only people the encrypted
	// record reaches) joins as THEMSELVES, full identity and E2EE, no knock.
	// We just created the room, so we are its owner and the mint cannot be a
	// permission problem; failing here still tears the room down, because a
	// room only guests can enter is not the feature.
	code, err := s.InviteCode(room.ID)
	if err != nil {
		_ = s.deleteGuildLocal(room.ID)
		s.dropGuestTokens(room.ID)
		return domain.Event{}, err
	}

	// Seed context: an arriving guest should know what they walked into
	// without anyone having to say it. One system message — title, time,
	// details — which the guest history replay (and the room itself) shows.
	eventEnd, _ := eventGuestRoomSpan(ev)
	s.sendSystem(room.Channels[0].ID, eventGuestSeed(ev, eventEnd))

	s.eventGuestMu.Lock()
	s.eventGuests[eventID] = eventGuestRecord{
		EventID: eventID, GuildID: guildID, MeetingGuildID: room.ID,
		AutoAdmit: autoAdmit, ExpiresUnix: expiry.Unix(),
	}
	s.eventGuestMu.Unlock()
	s.saveEventGuests()

	updated, err := s.stampEventGuest(groupID, ev, url, code)
	if err != nil {
		// The event write is what publishes the link; without it the room is an
		// orphan nobody can reach — take it back down.
		s.teardownEventGuestRoom(eventID)
		return domain.Event{}, err
	}
	s.emitGuildUpdate()
	return updated, nil
}

// eventGuestSeed renders the opening system message a guest reads first.
// Details are truncated, not sanitized further: they passed validEvent (no
// control characters) and travel to guests over the same capped frames every
// other message does.
func eventGuestSeed(ev domain.Event, end time.Time) string {
	start := time.Unix(ev.StartUnix, 0)
	when := start.Format("Mon, Jan 2 · 15:04")
	if ev.EndUnix > 0 {
		when += " – " + end.Format("15:04")
	}
	msg := "📅 " + ev.Title + "\n🕒 " + when
	if ev.Location != "" {
		msg += "\n📍 " + ev.Location
	}
	if d := ev.Details; d != "" {
		if utf8.RuneCountInString(d) > maxEventGuestSeedRunes {
			d = strings.TrimSpace(string([]rune(d)[:maxEventGuestSeedRunes])) + "…"
		}
		msg += "\n\n" + d
	}
	return msg
}

// stampEventGuest writes both doors onto the event — the guest link and the
// member join code — and announces it over the same upsert lane every edit
// rides, so members see the room through the signal they already listen to.
func (s *Service) stampEventGuest(groupID []byte, ev domain.Event, url, code string) (domain.Event, error) {
	ev.GuestURL = url
	ev.GuestHost = s.id.Fingerprint()
	ev.MemberCode = code
	if now := time.Now().Unix(); now > ev.UpdatedAt {
		ev.UpdatedAt = now
	} else {
		ev.UpdatedAt++ // same same-second tiebreak every edit gets
	}
	if err := validEvent(ev); err != nil {
		return domain.Event{}, err
	}
	if err := s.store.SaveEvent(ev); err != nil {
		return domain.Event{}, err
	}
	s.emitGuildUpdate()
	s.publishEvent(groupID, ev)
	return ev, nil
}

// RevokeEventGuests closes an event to guests: the link stops answering, the
// room is torn down, and the event sheds its "guests can join" state. Only
// the GUEST HOST's account may revoke — the room and its tokens exist on
// their node alone, so anyone else's "revoke" would be a lie the link
// outlives (a moderator who wants it gone can delete the event, which tears
// the room down on the host's node through the same hook).
func (s *Service) RevokeEventGuests(guildID, eventID string) (domain.Event, error) {
	groupID, ok := s.eventGroup(guildID)
	if !ok {
		return domain.Event{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	ev, found, err := s.store.EventByID(eventID)
	if err != nil {
		return domain.Event{}, err
	}
	if !found || ev.GuildID != guildID {
		return domain.Event{}, fmt.Errorf("app: unknown event %s", eventID)
	}
	if ev.GuestURL == "" {
		return ev, nil // nothing to revoke; not an error worth surfacing
	}
	if ev.GuestHost != s.id.Fingerprint() {
		return domain.Event{}, fmt.Errorf("app: only whoever opened the event to guests can revoke the link")
	}

	// Tear down first: the record going away is what makes the link answer
	// "no longer valid" even if the event write below fails. Deleting the room
	// also kills the member code — redeeming it dials a guild this node no
	// longer serves — so clearing all three fields below states a truth.
	s.teardownEventGuestRoom(eventID)

	ev.GuestURL, ev.GuestHost, ev.MemberCode = "", "", ""
	if now := time.Now().Unix(); now > ev.UpdatedAt {
		ev.UpdatedAt = now
	} else {
		ev.UpdatedAt++
	}
	if err := s.store.SaveEvent(ev); err != nil {
		return domain.Event{}, err
	}
	s.emitGuildUpdate()
	s.publishEvent(groupID, ev)
	return ev, nil
}

// teardownEventGuestRoom deletes the room this node hosts for an event, along
// with its guest tokens and the record. Safe to call when there is none.
// Reached from revoke, from event deletion (local AND the receive side — a
// moderator elsewhere deleting the event must kill the link too, and this
// node is the only one that can), and from the guest-field clear a linked
// device publishes.
func (s *Service) teardownEventGuestRoom(eventID string) {
	s.eventGuestMu.Lock()
	rec, ok := s.eventGuests[eventID]
	if ok {
		delete(s.eventGuests, eventID)
	}
	s.eventGuestMu.Unlock()
	if !ok {
		return
	}
	s.saveEventGuests()
	_ = s.deleteGuildLocal(rec.MeetingGuildID)
	s.dropGuestTokens(rec.MeetingGuildID)
	s.emitGuildUpdate()
}

// JoinEventRoom walks a MEMBER into an event's meeting room as themselves —
// the Teams distinction: people already inside the guild/DM click Join and
// walk straight in, only outsiders ride the guest link and knock. The code
// being redeemed arrived on the event's MLS-encrypted record, gated on
// receive so only the recorded host could have put it there; redeeming it is
// the ordinary invite handshake to the host's node, so the joiner becomes a
// real member of the room — own identity, full E2EE, never kind "guest" —
// and the guest knock cannot apply to them by construction (it lives in
// serveGuest, a path a member join never touches).
func (s *Service) JoinEventRoom(guildID, eventID string) (domain.Guild, error) {
	ev, found, err := s.store.EventByID(eventID)
	if err != nil {
		return domain.Guild{}, err
	}
	if !found || ev.GuildID != guildID {
		return domain.Guild{}, fmt.Errorf("app: unknown event %s", eventID)
	}
	// The HOST holds the room locally and owns its guild — no invite code is
	// needed or wanted. This runs BEFORE the memberCode gate so the host can
	// always join their own room, even one opened before member Join shipped;
	// and while we are here, heal a code-less room (OpenEventGuests is
	// idempotent) so members can join it too, closing the migration gap without
	// the host having to know it exists.
	s.eventGuestMu.Lock()
	rec, hosting := s.eventGuests[eventID]
	s.eventGuestMu.Unlock()
	if hosting && rec.MeetingGuildID != "" {
		s.mu.RLock()
		room, have := s.guilds[rec.MeetingGuildID]
		s.mu.RUnlock()
		if have {
			if ev.MemberCode == "" {
				_, _ = s.OpenEventGuests(guildID, eventID, rec.AutoAdmit)
			}
			return *room, nil
		}
	}
	if ev.MemberCode == "" {
		if ev.GuestURL != "" {
			return domain.Guild{}, fmt.Errorf("app: this room predates one-tap Join — ask the host to re-open it")
		}
		return domain.Guild{}, fmt.Errorf("app: this event has no room to join")
	}
	ic, err := decodeInviteCode(ev.MemberCode)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: this event's join code is unreadable — ask the host to re-open the room")
	}
	// Already inside — the host who minted the room, or a member who joined
	// earlier and is coming back: nothing to redeem, just hand the room over.
	s.mu.RLock()
	room, have := s.guilds[ic.GuildID]
	s.mu.RUnlock()
	if have && s.guildHasMember(ic.GuildID, s.id.Fingerprint()) {
		return *room, nil
	}
	joined, err := s.JoinViaInvite(ev.MemberCode)
	if err != nil {
		// The host answering "unknown guild" means the room was torn down
		// (revoked, expired, or the event died) and this member's copy of the
		// event simply hasn't caught up — say what actually happened.
		if strings.Contains(err.Error(), "unknown guild") {
			return domain.Guild{}, fmt.Errorf("app: this event's room has ended")
		}
		return domain.Guild{}, err
	}
	return joined, nil
}

// syncEventGuestExpiry follows an applied edit's times when this node hosts
// the event's room: "we moved the party an hour later" must not strand guests
// outside an expired link. Runs on both the local edit path and the receive
// path — whichever side moved the event, the room keeps up.
func (s *Service) syncEventGuestExpiry(ev domain.Event) {
	s.eventGuestMu.Lock()
	rec, ok := s.eventGuests[ev.ID]
	_, expiry := eventGuestRoomSpan(ev)
	changed := ok && rec.ExpiresUnix != expiry.Unix()
	if changed {
		rec.ExpiresUnix = expiry.Unix()
		s.eventGuests[ev.ID] = rec
	}
	s.eventGuestMu.Unlock()
	if !changed {
		return
	}
	s.saveEventGuests()
	s.setMeetingExpiry(rec.MeetingGuildID, expiry)
	// Refresh the token's stamped expiry too (same token, new lifetime).
	_, _ = s.CreateGuestLink(rec.MeetingGuildID, 0)
}
