package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Game search backs the collection editor's autocomplete: you type a title,
// we suggest real games with real box art. Queries go to
// Steam's public storefront search — proxied through the backend so the
// webview never talks to a third party directly — and results are cached
// briefly since autocomplete re-asks for every keystroke.
//
// Proxying through the backend keeps Valve from seeing a browser fingerprint,
// but the backend runs on the user's own machine, so it is still their IP and
// still their half-typed query. That is why this is gated: SearchGames returns
// nothing at all unless the game-search switch is on (offdevice.go), and it is
// off for every install that had not already been using the editor.

const gameSearchURL = "https://store.steampowered.com/api/storesearch/?l=english&cc=US&term="

// GameSearchResult is one autocomplete suggestion.
type GameSearchResult struct {
	Name  string `json:"name"`
	Cover string `json:"cover,omitempty"` // portrait box art (library_600x900)
	Thumb string `json:"thumb,omitempty"` // small landscape capsule for the dropdown row
}

var gameSearchClient = &http.Client{Timeout: 8 * time.Second}

// gameSearchCache memoizes recent queries (autocomplete hammers this).
var (
	gameSearchMu    sync.Mutex
	gameSearchCache = map[string]gameSearchEntry{}
)

type gameSearchEntry struct {
	at      time.Time
	results []GameSearchResult
}

const gameSearchTTL = 10 * time.Minute

// SearchGames suggests real games (name + box art) for a partial title.
// Best-effort: network trouble returns an empty list, never an error the UI
// has to explain — the editor falls back to free-text entry.
func (s *Service) SearchGames(query string) []GameSearchResult {
	// The consent gate, ahead of the cache as well as the request: a cached
	// answer is still an answer to a question this install has been told not to
	// ask, and serving one after the switch was flipped would make the switch
	// look broken. See offdevice.go.
	if !s.GameSearchEnabled() {
		return nil
	}
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return nil
	}
	query = clampBytes(query, maxGameNameBytes)
	key := strings.ToLower(query)

	gameSearchMu.Lock()
	if e, ok := gameSearchCache[key]; ok && time.Since(e.at) < gameSearchTTL {
		gameSearchMu.Unlock()
		return e.results
	}
	gameSearchMu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gameSearchURL+url.QueryEscape(query), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "concord-game-search")
	resp, err := gameSearchClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Items []struct {
			ID        json.Number `json:"id"`
			Name      string      `json:"name"`
			TinyImage string      `json:"tiny_image"`
		} `json:"items"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 256<<10)).Decode(&body); err != nil {
		return nil
	}

	out := make([]GameSearchResult, 0, len(body.Items))
	for _, it := range body.Items {
		name := strings.TrimSpace(it.Name)
		if name == "" || it.ID.String() == "" {
			continue
		}
		name = clampBytes(name, maxGameNameBytes)
		r := GameSearchResult{
			Name: name,
			// Portrait "library" art — the tall, pretty tile. Some titles
			// lack it; the UI falls back to the generated cover on load error.
			Cover: fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%s/library_600x900.jpg", it.ID),
		}
		if validGameCover(it.TinyImage) {
			r.Thumb = it.TinyImage
		}
		out = append(out, r)
		if len(out) == 8 {
			break
		}
	}

	gameSearchMu.Lock()
	gameSearchCache[key] = gameSearchEntry{at: time.Now(), results: out}
	if len(gameSearchCache) > 200 {
		for k := range gameSearchCache { // crude reset; cache is a courtesy
			delete(gameSearchCache, k)
		}
	}
	gameSearchMu.Unlock()
	return out
}
