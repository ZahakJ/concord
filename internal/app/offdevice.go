package app

// Off-device search consent.
//
// Almost everything Concord sends leaves for a peer or for the rendezvous the
// user picked. Two features are not like that: they take a string the user is
// still typing and hand it to somebody else's search engine. Neither had a
// switch, which meant PRIVACY.md had to describe them as things that simply
// happen — and a privacy document that says "this always happens, whatever your
// settings say" is describing a missing feature, not a design.
//
//   - Game search sends each keystroke burst in the collection editor to
//     Valve's public storefront search (games.go).
//   - GIF search sends the terms to the user's own rendezvous, which asks a
//     GIF provider on their behalf (gifsearch.go). The provider learns nothing
//     about the user; the rendezvous operator learns the terms.
//
// Both switches live here rather than in the frontend, because the frontend is
// not where the request is made. A preference in localStorage guarding a Go
// process that still holds the code to call Valve is a promise, not a gate: the
// call is one RPC away for anything running in the webview. Enforced at the top
// of the two Service methods, "off" means the request cannot be made at all,
// which is the thing the privacy policy is allowed to claim.
//
// The defaults differ, and the difference is the point:
//
//   - GIF search defaults ON. It reaches exactly one machine the user already
//     chose to route their traffic through and already trusts with their
//     connection metadata. Defaulting it off would be privacy theatre — it
//     would break a working feature to protect the user from a party they are
//     already talking to.
//   - Game search defaults OFF, because Valve is a third party nobody in this
//     conversation picked, and per-keystroke autocomplete is the most
//     talkative shape a query can take.
//
// …except that "default off" applied to an install that has been using game
// search for months is not a default, it is a removal. So the first time this
// runs against a keystore that has no recorded answer, it looks at whether the
// account has a game collection at all and writes down a decision: somebody
// with games has demonstrably used the editor and keeps the feature; a fresh
// install has not and starts closed. That decision is persisted immediately, so
// it is made once, on evidence, and never re-derived from a collection that
// changes later.

const (
	// gameSearchPrefKey and gifSearchPrefKey hold "1" or "0" in the settings
	// table. An empty read means "never decided" — see gameSearchDefault.
	gameSearchPrefKey = "game_search"
	gifSearchPrefKey  = "gif_search"
	// gamesSettingKey is the collection itself (service.go). Named here because
	// the migration below reads it, and a typo in one of two string literals
	// would silently turn the feature off for everybody who has it.
	gamesSettingKey = "games"
)

// gameSearchDefault decides the game-search switch from the two raw settings
// values it depends on, and reports whether that decision is new and therefore
// has to be written down.
//
// Pure on purpose: the migration is the part of this that can only be got wrong
// once, on somebody else's machine, so it is a function of two strings that a
// test can enumerate rather than a sequence of store calls.
func gameSearchDefault(pref, games string) (on, persist bool) {
	switch pref {
	case "1":
		return true, false
	case "0":
		return false, false
	}
	// Never decided. An existing collection is the evidence that this account
	// has used the editor; anything else — no key, empty JSON array, unparsable
	// leftovers — is not.
	return len(decodeGames(games)) > 0, true
}

// loadOffDeviceSearchPrefs mirrors both switches into memory at startup,
// running the game-search migration exactly once if it has never run.
func (s *Service) loadOffDeviceSearchPrefs() {
	gifPref, _ := s.store.GetSetting(gifSearchPrefKey)
	gamePref, _ := s.store.GetSetting(gameSearchPrefKey)
	games, _ := s.store.GetSetting(gamesSettingKey)

	gameOn, persist := gameSearchDefault(gamePref, games)
	if persist {
		// Best-effort: a failed write costs nothing but a second migration on
		// the next launch, which reaches the same answer from the same evidence.
		_ = s.store.SetSetting(gameSearchPrefKey, boolSetting(gameOn))
	}

	s.mu.Lock()
	s.gameSearchOn = gameOn
	s.gifSearchOn = gifPref != "0" // default ON; see the note above
	s.mu.Unlock()
}

// boolSetting renders a switch for the settings table.
func boolSetting(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// GameSearchEnabled reports whether the collection editor may ask Valve for
// title suggestions.
func (s *Service) GameSearchEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gameSearchOn
}

// SetGameSearchEnabled persists the switch. Turning it off does not retract
// anything already sent — it stops the next keystroke.
func (s *Service) SetGameSearchEnabled(on bool) error {
	if err := s.store.SetSetting(gameSearchPrefKey, boolSetting(on)); err != nil {
		return err
	}
	s.mu.Lock()
	s.gameSearchOn = on
	s.mu.Unlock()
	return nil
}

// GifSearchEnabled reports whether the GIF picker's Search tab may send terms
// to the rendezvous.
func (s *Service) GifSearchEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gifSearchOn
}

// SetGifSearchEnabled persists the switch. The guild's own GIF pack is
// unaffected: it never left the device to begin with.
func (s *Service) SetGifSearchEnabled(on bool) error {
	if err := s.store.SetSetting(gifSearchPrefKey, boolSetting(on)); err != nil {
		return err
	}
	s.mu.Lock()
	s.gifSearchOn = on
	s.mu.Unlock()
	return nil
}
