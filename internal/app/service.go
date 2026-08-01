package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/sync/singleflight"

	"github.com/zahak/concord/internal/crypto/mls"
	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
	cnet "github.com/zahak/concord/internal/net"
	"github.com/zahak/concord/internal/store"
)

// Service is the orchestration layer (layer 6): it owns the identity, network
// host, gossipsub bus, MLS group-crypto engine, and encrypted store, and
// presents one UI-agnostic API. Both the headless CLI and the Wails GUI drive
// Concord exclusively through a Service.
type Service struct {
	ctx     context.Context
	dataDir string

	id    *identity.Identity
	host  *cnet.Host
	ps    *cnet.PubSub
	mls   mls.Engine
	store *store.Store
	// peers remembers where we reached other peers, so the next launch can dial
	// them directly instead of depending on the rendezvous being alive.
	peers *PeerCache

	mu             sync.RWMutex
	guilds         map[string]*domain.Guild // by guild ID
	channelToGuild map[string]string        // channel ID -> guild ID
	onMessage      []func(domain.Message)

	// inviteMu serializes the add-and-publish critical section of
	// handleInviteRequest. The owner is the sole committer, but libp2p runs one
	// handler goroutine per inbound stream, so two joiners dialing at once (e.g.
	// a group DM fanning invites to several people) would otherwise produce two
	// commits at the same epoch. Serializing keeps each add on its own epoch.
	inviteMu sync.Mutex

	// pendingDMInvites tracks DM invitees (1:1 and group) who haven't joined
	// yet (guild ID -> set of fingerprints). When such a peer later connects we
	// push them the invite, and the heal loop re-pushes periodically, so a DM
	// eventually reaches everyone even if they were offline (or the push was
	// lost) when it was made. Persisted in the settings table (see dmState).
	dmInviteMu       sync.Mutex
	pendingDMInvites map[string]map[string]bool

	// requests holds message requests from strangers (fingerprint -> the invite
	// we are deliberately NOT redeeming yet; see request.go). Its own mutex: the
	// tray is written from the invite handler, which already calls helpers that
	// take mu. Persisted in the settings table.
	reqMu    sync.Mutex
	requests map[string]MessageRequest

	// hiddenDMs marks DM conversations the user has closed. Discord-style: the
	// conversation stays fully alive (MLS membership, subscriptions, history)
	// but the UI hides it until the user reopens it or a new message arrives.
	// Guarded by mu; persisted in the settings table (see dmState).
	hiddenDMs map[string]bool

	// dmPeers records which peer a 1:1 DM was created FOR (guild ID ->
	// fingerprint). It keeps the conversation identifiable even while the peer
	// hasn't joined yet (or has left), so StartDM re-opens it instead of
	// minting a duplicate. Guarded by mu; persisted (see dmState).
	dmPeers map[string]string

	// Rich presence: an auto-detected "now playing" line that overlays the manual
	// status while something is playing. activity is the current overlay (empty =
	// use the manual status); richPresenceStop cancels the poller when disabled.
	activityMu       sync.Mutex
	activity         string
	activityInfo     *Activity
	richPresenceStop context.CancelFunc

	// Background mode (see background.go): bg is whether the mobile shell says
	// the app is off screen; bgWake is closed on return to foreground so paced
	// loops fire immediately instead of waiting out a stretched interval.
	bgMu   sync.Mutex
	bg     bool
	bgWake chan struct{}

	voiceMu    sync.Mutex
	voiceRooms map[string]context.CancelFunc // channel ID -> heartbeat stop (rooms we're IN)
	// voiceWatched marks voice channels whose presence topic we passively listen
	// to (for every voice channel in every guild), so the sidebar can show who's
	// in a call without us having to join it — Discord-style guild-wide presence.
	voiceWatched    map[string]bool
	onVoicePresence []func(from, fingerprint, channelID, action, target, dest string)
	onVoiceSignal   []func(from string, data []byte)

	onTyping      []func(from, channelID string)
	onGuildUpdate []func()
	onGuildInvite []func(GuildInvite)
	onReadState   []func(channelID string, at int64)

	// Read markers awaiting broadcast to our own devices. Coalesced (see
	// broadcastReadMarker) so a mark-all-read burst becomes ONE publish.
	readMarkMu      sync.Mutex
	pendingReadMark map[string]int64
	readMarkTimer   *time.Timer

	// Browser-guest sessions (see guest.go): issued meeting tokens, the live
	// sessions per channel, and the same sessions by id — a guest in a call is
	// addressed as the voice peer "guest:<id>".
	guestMu       sync.Mutex
	guestTokens   map[string]guestToken
	guestSessions map[string][]*guestSession
	guestByID     map[string]*guestSession
	// guestDoor is the instant a channel's guest door stops being locked. A lock
	// is re-announced every few seconds while it is on (see the front end's
	// toggleCallLock), so treating it as a lease rather than a flag makes a
	// crashed or reloaded host unlock itself instead of leaving a door nobody
	// alive can open. Guarded by guestMu.
	guestDoor map[string]time.Time

	// meetingLife holds the chosen expiry of each instant meeting (guild ID →
	// instant). Absent means the legacy fixed meetingTTL after creation.
	// Guarded by mu, persisted under meetingLifetimeKey.
	meetingLife map[string]time.Time

	// Public booking page (see booking.go): the host's availability config,
	// the taken slots, and the receive-side rate budgets for the relayed
	// /concord/booking/1.0.0 requests. All guarded by bookingMu.
	bookingMu          sync.Mutex
	bookingCfg         bookingConfig
	bookingRecords     []bookingRecord
	bookingSlotsBucket tokenBucket
	bookingBookBucket  tokenBucket

	// Guest-opened calendar events (see eventguest.go): event ID → the
	// disposable meeting room this node hosts for it. Guarded by eventGuestMu,
	// persisted under eventGuestsKey. Local-only by design — the room, its
	// tokens and its door policy exist on the minting node alone.
	eventGuestMu sync.Mutex
	eventGuests  map[string]eventGuestRecord

	profiles map[string]Profile // fingerprint -> profile, learned from peers

	// nicks holds per-guild display-name overrides: guildID -> fingerprint ->
	// nickname. A nickname shadows the global profile name inside that guild only
	// (Discord-style server nicknames). Members set their own; propagated over
	// the guild-meta topic like profiles.
	nicks map[string]map[string]string

	// Governance (roles/permissions/bans). govOps is the per-guild signed op log;
	// govState is the folded result (rebuilt on every change) that the committer
	// gate and invite gate consult. Both are guarded by mu.
	govOps   map[string][]govOp
	govState map[string]GuildState
	// govHashes indexes each guild's ingested op hashes for O(1) dedup, instead
	// of re-hashing the whole op log on every ingest (sync replays the full log).
	govHashes map[string]map[string]bool

	// outOfSync marks guilds whose MLS epoch gap could not be bridged by any
	// peer's commit log (see sync.go); the UI surfaces a re-invite hint.
	outOfSync map[string]bool
	// Last time an undecryptable message flagged a group, so a channel full of
	// unreadable traffic raises the alarm once per heal cycle rather than on
	// every packet. See flagUndecryptable.
	lastUndecryptable map[string]time.Time
	// pendingCT holds ciphertexts that arrived before the commit that would let
	// us read them (guarded by pendingCTMu, keyed by group ID). Gossip won't
	// redeliver a message, so without this a message that races its own
	// membership commit by a few milliseconds is lost until a full history
	// sync. Retried whenever the group's epoch advances; see receiveCiphertext.
	pendingCT   map[string][]pendingCipher
	pendingCTMu sync.Mutex
	// recovering dedupes concurrent recoverOutOfSync runs per guild (guarded by
	// pendingCTMu, which is convenient and uncontended).
	recovering map[string]bool
	// joining serializes JoinViaInvite per guild (guarded by joiningMu); see
	// claimJoin for why concurrent duplicate joins are actively harmful.
	joining   map[string]*sync.Mutex
	joiningMu sync.Mutex

	// blocked is the in-memory mirror of the block list (see block.go), guarded
	// by mu. A blocked account's DM/guild invites are dropped on arrival.
	blocked map[string]bool

	// typingOn mirrors the typing-indicator preference (see typing.go), guarded
	// by mu — it is read on every keystroke's publish and on every inbound hint.
	typingOn bool

	// devices records which device keys we have seen behind each contact's
	// account (fingerprint -> set of hex device keys; see devicewatch.go),
	// guarded by mu. A key appearing that isn't in the set is a device that was
	// linked into their account since we last looked.
	devices map[string]map[string]bool

	// pendingMembers[guildID][fingerprint] = people you've added to a guild who
	// haven't joined yet — shown as "pending" in the roster (like a DM you've
	// opened). Guarded by mu; persisted; cleared once they actually join.
	pendingMembers map[string]map[string]bool

	// attachFlight collapses concurrent fetches of one attachment blob (e.g.
	// the same image rendered several times) into a single network request.
	attachFlight singleflight.Group

	// previews caches link-preview scrapes (see preview.go).
	previews *previewCache

	// Mailbox: X25519 keypair for sealing offline envelopes, and the parsed
	// rendezvous nodes that host our mailbox (see mailbox.go).
	mbxPriv [32]byte
	mbxPub  [32]byte
	// mbxTick counts heal ticks so the mailbox sweep can run on a slower beat
	// than the loop it rides (see sweepMailbox). Only touched from that loop.
	mbxTick int
	// bootstrap is read from several goroutines and can be REPLACED at runtime
	// by SetBootstrapLive, so it is only ever touched through bootstrapPeers()
	// and setBootstrapPeers() under this lock.
	bootstrapMu sync.RWMutex
	bootstrap   []peer.AddrInfo

	// Device linking (issuer side): the secret of the currently-displayed link
	// offer, consumed once a joiner proves it. nil = no active offer. See link.go.
	linkMu     sync.Mutex
	linkSecret []byte

	// myCredential is this install's MLS leaf credential — the bare account key
	// (single-device) or this device's cert (linked). It's what we present when
	// joining a guild, so it matches our KeyPackage's leaf.
	myCredential []byte

	// deviceAccounts maps a linked device's public key (hex) to its account
	// fingerprint, learned from device certs seen in rosters/messages. It lets
	// presence() recover the ACCOUNT a device-keyed PeerID belongs to, so a
	// linked phone doesn't surface as a separate "cryptic" peer. Keyed by the
	// raw pubkey embedded in the PeerID.
	deviceMu       sync.RWMutex
	deviceAccounts map[string]string
	// revokedDevices are our own device keys we have unlinked (hex), guarded by
	// deviceMu. Kept in memory because learnDeviceCert consults it on every
	// inbound message; see unlink.go.
	revokedDevices map[string]bool

	// regMu guards the persisted own-device registry (devices.go). Separate from
	// deviceMu on purpose: the registry writes to the store, and the in-memory
	// device→account map is read on the network thread.
	regMu sync.Mutex

	// greeted tracks who we have introduced ourselves to on their current
	// connection (hello.go), so a reconnect greets again and a burst of notifier
	// events doesn't.
	greeted greetSet
	// answered is the responder-side counterpart: one composed reply per peer
	// per connection, so reopening the stream can't be used to make us decode
	// every guild's roster over and over.
	answered greetSet
	// solicited tracks the unplaceable peers we have asked to introduce
	// themselves (solicitHello), one ask per connection: a member device pays
	// once and is placed; an outsider spamming a gossip topic pays us nothing
	// more than that one refused stream.
	solicited greetSet

	// reaching holds the own-account devices we currently have a dial in flight
	// for (devices.go), so an unreachable phone costs one attempt at a time
	// rather than one per beat.
	reaching greetSet

	// onPeerUp/onPeerDown are the UI's presence feed. They are held here, rather
	// than handed straight to the host, because a peer can be placed LATE — a
	// linked device whose certificate arrives a moment after its connection — and
	// the event has to be able to fire then. presenceShown keeps that honest: one
	// up per connection, and a down only if an up was sent. Guarded by mu.
	onPeerUp      []func(PeerPresence)
	onPeerDown    []func(PeerPresence)
	presenceShown map[peer.ID]bool

	// onUnlinked fires once this device has been told it is unlinked and has
	// erased itself, so the shell can drop the session instead of leaving the UI
	// talking to a closed store. Guarded by mu.
	onUnlinked []func()
}

// Profile is a member's self-asserted presentation: display name, a short
// status line, an avatar (emoji and/or a small image), and an accent color.
// All decorative — the fingerprint remains the authenticated identity.
type Profile struct {
	Name   string `json:"name"`
	Status string `json:"status"` // short custom status line
	Emoji  string `json:"emoji"`
	Color  string `json:"color"`
	Avatar string `json:"avatar"` // small image as a data URI ("" = none)
	// Banner is a wide profile-header image (data URI, "" = none), like a guild
	// banner but for a person; shown on the profile card.
	Banner string `json:"banner,omitempty"`
	// Presence is the user-chosen availability: "" / "online" (default), "idle",
	// "dnd", or "invisible" (appear offline). It shades the avatar dot.
	Presence string `json:"presence,omitempty"`
	// Bio is a longer "about me" shown on the profile card.
	Bio string `json:"bio,omitempty"`
	// MailboxPub is this member's X25519 mailbox key, shared so others can seal
	// offline envelopes to them (see mailbox.go). Not user-facing.
	MailboxPub []byte `json:"mbx,omitempty"`
	// Activity is the structured rich-presence payload (now playing: art,
	// duration, position snapshot). Ephemeral — never persisted; old clients
	// ignore it and fall back to the 🎵 status string.
	Activity *Activity `json:"activity,omitempty"`
	// Games is the member's curated game collection (Discord-style), shown on
	// the profile card. A name plus an optional cover URL (validated against
	// the Steam CDN allowlist) — art stays a URL, keeping broadcasts tiny.
	Games []Game `json:"games,omitempty"`
	// Color2 pairs with Color to form the profile's gradient theme
	// (Nitro-style). Empty = single-color/default rendering.
	Color2 string `json:"color2,omitempty"`
	// Frame is a decorative avatar ring, by enum id (see validFrame) — art is
	// pure client-side CSS (several are ANIMATED), so the broadcast stays a
	// few bytes no matter how fancy it looks.
	Frame string `json:"frame,omitempty"`
	// Effect is a card-wide flourish by enum id (see validEffect): animated
	// gradient banners, sparkles, sheen. Also pure CSS.
	Effect string `json:"effect,omitempty"`
	// Style holds the fine-grained knobs (ring speed/direction/glow, banner
	// gradient angle…). One small struct so the profile broadcast gains one
	// short JSON object, not a dozen fields.
	Style *Style `json:"style,omitempty"`
	// UpdatedAt is when the profile's OWNER last edited it (UnixMilli; 0 =
	// legacy/unknown). It is what lets the account's own devices converge on
	// the newest edit (last-writer-wins) and what stops a stale relay from
	// rolling a profile back. Advisory metadata — never rendered.
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

// Style is the customization dial-set behind Frame/Effect. Every value is an
// enum or a bounded number — never free text or a URL — so a hostile peer's
// profile can't inject anything into the CSS we render it with.
type Style struct {
	// Ring animation.
	Speed string `json:"speed,omitempty"` // slow | normal | fast
	Dir   string `json:"dir,omitempty"`   // cw | ccw
	Glow  string `json:"glow,omitempty"`  // off | soft | strong
	Width int    `json:"width,omitempty"` // ring thickness, 1..5
	// Banner.
	Angle int    `json:"angle,omitempty"` // gradient angle, 0..360
	Fill  string `json:"fill,omitempty"`  // gradient | solid  (when no image)
	// The thing orbiting your avatar on an orbit ring: an emoji, or a 64px
	// sprite the user uploaded (data URI).
	Sat string `json:"sat,omitempty"`
	// The Gradient ring's colorway id (see frontend lib/rings.js PALETTES).
	Pal string `json:"pal,omitempty"`
}

// maxSatBytes caps the orbiting sprite. The frontend bakes it to 64×64 PNG
// (~2-8KB); this leaves headroom without letting a peer ship a megabyte in a
// field that renders on everyone's screen.
const maxSatBytes = 32 * 1024

// validSat: a short emoji, or a small image data URI. Rejecting everything else
// matters because this string comes from PEERS and ends up in an <img src>.
func validSat(s string) bool {
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "data:") {
		return validImageDataURI(s, maxSatBytes)
	}
	return len(s) <= 16 && utf8.ValidString(s) && !strings.ContainsAny(s, "\"'<>\\;()")
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// sanitizeStyle bounds every dial (and drops the struct when it's all
// defaults, so a plain profile stays byte-identical to before).
func sanitizeStyle(st *Style) *Style {
	if st == nil {
		return nil
	}
	out := Style{}
	if oneOf(st.Speed, "slow", "normal", "fast") {
		out.Speed = st.Speed
	}
	if oneOf(st.Dir, "cw", "ccw") {
		out.Dir = st.Dir
	}
	if oneOf(st.Glow, "off", "soft", "strong") {
		out.Glow = st.Glow
	}
	if st.Width >= 1 && st.Width <= 5 {
		out.Width = st.Width
	}
	if st.Angle >= 0 && st.Angle <= 360 {
		out.Angle = st.Angle
	}
	if oneOf(st.Fill, "gradient", "solid") {
		out.Fill = st.Fill
	}
	if validSat(st.Sat) {
		out.Sat = st.Sat
	}
	if validID(st.Pal) {
		out.Pal = st.Pal
	}
	if out == (Style{}) {
		return nil
	}
	return &out
}

func stylesEqual(a, b *Style) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// validColor admits "" or a #hex color — profile colors render into inline
// CSS, so anything else from a peer is dropped.
var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

func validColor(c string) bool { return c == "" || hexColorRe.MatchString(c) }

// validFrame admits an avatar-ring id. The id is looked up in the client's
// ring table (frontend/src/lib/rings.js) and, like a banner preset, ends up
// inside CSS — so it is held to a strict charset here. An id this client
// doesn't know simply renders as no ring.
func validFrame(f string) bool {
	if f == "" {
		return true
	}
	if len(f) > 32 {
		return false
	}
	for _, r := range f {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

// validEffect admits the known profile-card effects.
func validEffect(e string) bool {
	switch e {
	case "", "aurora", "sparkle", "sheen", "nebula":
		return true
	}
	return false
}

// sanitizeProfileExtras bounds the newer decorative fields, for our own edits
// and peers' broadcasts alike.
func sanitizeProfileExtras(p *Profile) {
	// Both accent colors render into inline CSS (Avatar, ProfilePopover). An
	// unvalidated primary Color like "red;background-image:url(https://evil/px)"
	// is a stored-CSS injection that fires an external fetch — deanonymizing every
	// viewer's IP — the moment the member list opens. Hold Color to the same #hex
	// gate as Color2.
	if !validColor(p.Color) {
		p.Color = ""
	}
	if !validColor(p.Color2) {
		p.Color2 = ""
	}
	if !validFrame(p.Frame) {
		p.Frame = ""
	}
	if !validEffect(p.Effect) {
		p.Effect = ""
	}
	p.Style = sanitizeStyle(p.Style)
}

// Game is one entry in a member's game collection.
type Game struct {
	Name  string `json:"name"`
	Cover string `json:"cover,omitempty"` // https box-art URL (Steam CDN only)
}

// maxGames caps the game collection — generous (a library, not a top-10), but
// still bounded: entries ride every profile broadcast, and a worst-case entry
// (64B name + 300B cover URL) × 100 stays ~36KB, well inside the gossip frame
// budget. maxGameNameBytes bounds each name; maxGameCoverBytes the cover URL.
const (
	maxGames          = 100
	maxGameNameBytes  = 64
	maxGameCoverBytes = 300
)

// validArtURL admits album-art URLs only from known music CDNs (https). The
// allowlist is what lets clients render art by DEFAULT, Discord-style: a peer
// broadcasting an Activity cannot point ArtURL at a host they control and
// harvest the IP of everyone who views their profile. Anything else is
// dropped — the card falls back to the 🎵 placeholder.
func validArtURL(u string) bool {
	if u == "" || len(u) > maxArtURLBytes || !strings.HasPrefix(u, "https://") {
		return false
	}
	rest := strings.TrimPrefix(u, "https://")
	host, _, _ := strings.Cut(rest, "/")
	host, _, _ = strings.Cut(strings.ToLower(host), ":") // strip any port
	for _, suffix := range []string{
		".scdn.co",                   // Spotify
		".spotifycdn.com",            // Spotify
		".ytimg.com",                 // YouTube
		".googleusercontent.com",     // YouTube Music / Google
		".mzstatic.com",              // Apple Music
		".bcbits.com",                // Bandcamp
		".sndcdn.com",                // SoundCloud
		".coverartarchive.org",       // MusicBrainz
		".archive.org",               // MusicBrainz art storage
		".steamstatic.com",           // Steam (game soundtracks)
		".fanart.tv",                 // Kodi/Plex scrapers
		".plex.direct",               // Plex
		".last.fm",                   // Last.fm
		".lastfm.freetls.fastly.net", // Last.fm CDN
	} {
		if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	return false
}

// validBanner admits an image data URI or a preset id ("preset:galaxy"). The
// preset id is rendered into CSS on every viewer's machine, so it is held to
// a strict charset — a peer must never be able to smuggle CSS through it.
// validID admits the short lowercase ids we mint ourselves (ring palettes,
// banner presets). Peers send these and we interpolate them into CSS class
// names / lookups, so anything outside [a-z0-9-] is rejected outright.
func validID(id string) bool {
	if id == "" {
		return true
	}
	if len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

// validImageDataURI enforces a STRICT data-URI shape: a known raster image type
// followed by base64. This matters because a banner is interpolated into a CSS
// url("…") — a value like `data:image/svg+xml,x");background:url(//attacker)` is
// still "data:image/…" but breaks out of the quotes and injects CSS that fetches
// an attacker URL (deanonymizing every viewer). Restricting to base64 raster
// images admits exactly what the app itself produces (canvas.toDataURL) while
// making the quote/paren/semicolon breakout characters impossible.
func validImageDataURI(u string, maxLen int) bool {
	if len(u) > maxLen {
		return false
	}
	rest, ok := strings.CutPrefix(u, "data:image/")
	if !ok {
		return false
	}
	meta, b64, ok := strings.Cut(rest, ",")
	if !ok {
		return false
	}
	typ, enc, ok := strings.Cut(meta, ";")
	if !ok || enc != "base64" {
		return false
	}
	if !oneOf(typ, "png", "jpeg", "jpg", "gif", "webp") {
		return false
	}
	for _, r := range b64 {
		if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') &&
			!(r >= '0' && r <= '9') && r != '+' && r != '/' && r != '=' {
			return false
		}
	}
	return b64 != ""
}

func validBanner(b string) bool {
	if b == "" {
		return true
	}
	if strings.HasPrefix(b, "data:") {
		return validImageDataURI(b, maxProfileBannerBytes)
	}
	id, ok := strings.CutPrefix(b, "preset:")
	if !ok || id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

// validGameCover admits only https images on Steam's CDNs. This is a security
// gate, not pedantry: covers arrive in PEERS' profile broadcasts and render as
// <img> for everyone, so without an allowlist a peer could plant a tracking
// URL that leaks every viewer's IP to a host they control.
func validGameCover(u string) bool {
	if u == "" || len(u) > maxGameCoverBytes {
		return false
	}
	for _, prefix := range []string{
		"https://cdn.cloudflare.steamstatic.com/",
		"https://cdn.akamai.steamstatic.com/",
		"https://shared.cloudflare.steamstatic.com/",
		"https://shared.akamai.steamstatic.com/",
		"https://store.steampowered.com/",
	} {
		if strings.HasPrefix(u, prefix) {
			return true
		}
	}
	return false
}

// sanitizeGames normalizes a game collection: trimmed, non-empty, deduped
// (case-insensitively), names bounded, covers allowlisted, at most maxGames
// entries. Applied to our own edits and to peers' broadcasts alike.
func sanitizeGames(games []Game) []Game {
	seen := map[string]bool{}
	var out []Game
	for _, g := range games {
		g.Name = strings.TrimSpace(g.Name)
		if g.Name == "" {
			continue
		}
		if len(g.Name) > maxGameNameBytes {
			g.Name = g.Name[:maxGameNameBytes]
		}
		if !validGameCover(g.Cover) {
			g.Cover = ""
		}
		key := strings.ToLower(g.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, g)
		if len(out) == maxGames {
			break
		}
	}
	return out
}

// encodeStyle/decodeStyle persist the style dials as one small JSON blob.
func encodeStyle(st *Style) string {
	st = sanitizeStyle(st)
	if st == nil {
		return ""
	}
	b, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeStyle(raw string) *Style {
	if raw == "" {
		return nil
	}
	var st Style
	if json.Unmarshal([]byte(raw), &st) != nil {
		return nil
	}
	return sanitizeStyle(&st)
}

// decodeGames parses a persisted game list. It tolerates the pre-cover format
// (a bare JSON array of name strings) so nothing is lost across the upgrade.
func decodeGames(raw string) []Game {
	if raw == "" {
		return nil
	}
	var games []Game
	if json.Unmarshal([]byte(raw), &games) == nil && len(games) > 0 && games[0].Name != "" {
		return games
	}
	var names []string
	if json.Unmarshal([]byte(raw), &names) == nil {
		for _, n := range names {
			games = append(games, Game{Name: n})
		}
		return games
	}
	return nil
}

// maxAvatarBytes caps the avatar data URI so profile broadcasts stay far below
// the gossipsub frame limit (the UI downscales to ~96px JPEG, typically <10 KB).
const maxAvatarBytes = 64 * 1024

// maxProfileBannerBytes caps a user's profile banner. Larger than the avatar
// (it's a wide header) but still bounded so profile broadcasts stay reasonable;
// the UI downscales before sending.
const maxProfileBannerBytes = 256 * 1024

// maxNameBytes bounds self-asserted display names and per-guild nicknames so a
// peer can't publish a pathologically long string over the meta topic.
const maxNameBytes = 64

// maxBioBytes bounds the profile "about me" so profile broadcasts stay small.
const maxBioBytes = 600

// PeerPresence is a UI-facing view of a connected peer.
type PeerPresence struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
}

// Config configures a Service.
type Config struct {
	// DataDir is where the keystore and database live.
	DataDir string
	// Passphrase unlocks the identity keystore.
	Passphrase string
	// DisableMDNS turns off LAN discovery; tests set it for determinism.
	DisableMDNS bool
	// BootstrapPeers are multiaddrs of rendezvous/relay nodes for internet-wide
	// discovery. When any are set, the DHT is enabled.
	BootstrapPeers []string

	// listenAddrs and blockedIPs shape reachability for tests in this package:
	// pinning each device to its own loopback alias and blocking the other's
	// simulates two NATs whose only shared path is a relay circuit. Unexported
	// on purpose — no production caller can reach them.
	listenAddrs []string
	blockedIPs  []string
}

// Start loads (or creates) the identity, then brings up storage, networking
// (with LAN discovery), gossipsub, and the MLS engine, and restores any
// previously-joined guilds. The returned Service must be Closed by the caller.
func Start(ctx context.Context, cfg Config) (*Service, error) {
	id, _, err := identity.LoadOrCreate(keystorePathIn(cfg.DataDir), cfg.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("app: open identity: %w", err)
	}

	st, err := store.Open(dbPathIn(cfg.DataDir), deriveStoreKey(id))
	if err != nil {
		return nil, fmt.Errorf("app: open store: %w", err)
	}

	// Bootstrap sources, in precedence order: explicit config (env) first, then
	// the saved connection settings (set in-app on the login screen).
	netCfg := LoadNetConfig(cfg.DataDir)
	bootstrapAddrs := cfg.BootstrapPeers
	if len(bootstrapAddrs) == 0 {
		bootstrapAddrs = netCfg.Bootstrap
	}
	bootstrap, err := parseBootstrapPeers(bootstrapAddrs)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	// Peers we have met before are discovery we own outright: they need no
	// rendezvous, no DHT and no server, and they are why an existing group of
	// friends keeps working after the rendezvous dies.
	peerCache := LoadPeerCache(cfg.DataDir)
	remembered := peerCache.AddrInfos()
	// Linked-device mode: if a device marker is present and verifies, this
	// install uses its own device key for the libp2p PeerID and its device cert
	// as the MLS credential. Absent → default single-device behavior (account-key
	// PeerID, bare account credential), unchanged.
	marker, linked := loadDeviceMarker(cfg.DataDir, id.PublicKey())
	var hostKey ed25519.PrivateKey
	if linked {
		hostKey = id.DeviceKey()
	}
	host, err := cnet.New(ctx, cnet.Config{
		Identity:        id,
		HostKey:         hostKey,
		EnableMDNS:      !cfg.DisableMDNS,
		ListenPort:      netCfg.ListenPort,
		EnableDHT:       wantDHT(bootstrap, netCfg.PublicDHT, remembered),
		BootstrapPeers:  bootstrap,
		PublicBootstrap: netCfg.PublicDHT,
		RememberedPeers: remembered,
		ListenAddrs:     cfg.listenAddrs,
		BlockedIPs:      cfg.blockedIPs,
	})
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("app: start network: %w", err)
	}

	ps, err := host.NewPubSub(ctx)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: start pubsub: %w", err)
	}

	mlsDir, err := mlsDirIn(cfg.DataDir)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: mls storage dir: %w", err)
	}
	// Persistent MLS storage + a deterministic signing key give full restart
	// recovery: group state from disk, signing key reproduced, so a restarted
	// node can both receive and send. mlsIdentity selects the account-key
	// credential (default) or the device cert + device signing key (linked).
	mlsCred, mlsSigning := mlsIdentity(id, marker)
	engine, err := mls.NewPersistent(mlsCred, mlsSigning, mlsDir)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: start mls: %w", err)
	}

	s := &Service{
		ctx:              ctx,
		dataDir:          cfg.DataDir,
		id:               id,
		host:             host,
		ps:               ps,
		mls:              engine,
		myCredential:     mlsCred,
		store:            st,
		peers:            peerCache,
		guilds:           map[string]*domain.Guild{},
		channelToGuild:   map[string]string{},
		pendingDMInvites: map[string]map[string]bool{},
		hiddenDMs:        map[string]bool{},
		dmPeers:          map[string]string{},
		requests:         map[string]MessageRequest{},
		devices:          map[string]map[string]bool{},
		voiceRooms:       map[string]context.CancelFunc{},
		voiceWatched:     map[string]bool{},
		profiles:         map[string]Profile{},
		nicks:            map[string]map[string]string{},
		govOps:           map[string][]govOp{},
		govState:         map[string]GuildState{},
		govHashes:        map[string]map[string]bool{},
		meetingLife:      map[string]time.Time{},
		outOfSync:        map[string]bool{},
		blocked:          map[string]bool{},
		pendingMembers:   map[string]map[string]bool{},
		previews:         newPreviewCache(),
		bootstrap:        bootstrap,
	}
	s.mbxPriv, s.mbxPub = deriveMailboxKeys(id)

	// Before ANYTHING else reads the account: has this device been unlinked?
	// A previous session was told and did not finish erasing, or the record
	// survived a crash. Checked here rather than at the end of Start so a revoked
	// install never serves a roster, answers a sync, or joins a topic.
	s.loadRevoked()
	if s.selfRevoked() {
		s.wipeSelf()
		return nil, ErrDeviceUnlinked
	}

	// Restore learned member profiles so names survive restarts (they are also
	// repaired by invite handshakes and history sync, but this avoids a window
	// of fingerprint-only names right after unlock).
	if rows, err := st.Profiles(); err == nil {
		for _, r := range rows {
			s.profiles[r.Fingerprint] = Profile{Name: r.Name, Status: r.Status, Emoji: r.Emoji, Color: r.Color, Avatar: r.Avatar, Banner: r.Banner, Presence: r.Presence, Bio: r.Bio, MailboxPub: r.MailboxPub, Games: decodeGames(r.Games), Color2: r.Color2, Frame: r.Frame, Effect: r.Effect, Style: decodeStyle(r.Style)}
		}
	}

	// Stamp a profile that predates edit stamps (once). Every device-to-device
	// lane — link handover, device hello, sync roster — offers the profile with
	// its stamp and adopts only strictly newer, so a stampless (0) profile
	// would never travel anywhere. The RAW display_name setting, not
	// DisplayName(): that helper falls back to a fingerprint block, which reads
	// as "a name is set" on every install — stamping a genuinely blank device
	// here would crown it the account's newest editor and push its blankness
	// over every device that actually has the profile.
	if s.profileStamp() == 0 {
		rawName, _ := st.GetSetting("display_name")
		if p := s.selfStoredProfile(); strings.TrimSpace(rawName) != "" || p.Status != "" ||
			p.Emoji != "" || p.Color != "" || p.Color2 != "" || p.Avatar != "" ||
			p.Banner != "" || p.Presence != "" || p.Bio != "" || p.Frame != "" ||
			p.Effect != "" || len(p.Games) > 0 {
			s.bumpProfileStamp()
		}
	}

	// Restore per-guild nicknames so server-scoped names survive restarts.
	if nicks, err := st.Nicknames(); err == nil {
		s.nicks = nicks
	}

	// Restore governance op logs and fold them into per-guild state (roles/bans).
	if all, err := st.AllGuildOps(); err == nil {
		for gid, raw := range all {
			for _, b := range raw {
				var o govOp
				if json.Unmarshal(b, &o) == nil {
					s.govOps[gid] = append(s.govOps[gid], o)
					if s.govHashes[gid] == nil {
						s.govHashes[gid] = map[string]bool{}
					}
					s.govHashes[gid][o.hash()] = true
				}
			}
		}
	}

	// Owner side of the join handshake.
	host.HandleInvites(s.handleInviteRequest)

	// Issuer side of the device-linking handshake.
	host.HandleLink(s.handleLinkRequest)

	// "Here is which account I belong to." The one thing a linked device's PeerID
	// cannot say for itself — see hello.go.
	host.HandleHello(s.handleHello)

	// Serve history catch-up requests from reconnecting peers.
	host.HandleSync(s.handleSyncRequest)

	// Serve attachment blobs to peers rendering images we hold.
	host.HandleAttachments(s.handleAttachRequest)

	// Hand our own release binary to peers running an older one.
	host.HandleRelease(s.handleReleaseRequest)

	// Auto-redeem direct-message invitations pushed by a peer.
	host.HandleDMInvites(s.handleDMInvite)

	// Inbound WebRTC signaling for voice/video.
	host.HandleSignals(func(from peer.ID, data []byte) {
		s.emitVoiceSignal(from.String(), data)
	})

	// Trust-on-first-use: record every peer we connect to so it can later be
	// verified out-of-band. Also re-broadcast our display name to all guilds a
	// moment after any peer connects: the gossipsub mesh needs to warm up, so a
	// one-shot announce at join time can be missed — this makes names converge
	// reliably (each side re-announces, and learning a new profile triggers a
	// reply, so both peers end up with each other's names).
	host.OnPeerConnected(func(p peer.ID) {
		pp := s.presence(p)
		// The presence feed, gated on the peer being somebody we have (see
		// OnPeerConnected). A peer we cannot place yet is silent here and is
		// announced by peerResolved instead, the instant its hello lands.
		if s.knownContact(pp.Fingerprint) {
			s.emitPeerUp(pp)
		}
		// Introduce ourselves, so a device whose PeerID says nothing about its
		// account stops being a stranger within one round trip instead of
		// whenever unrelated traffic next happens to carry its certificate.
		go s.greet(p)
		// rememberPeer records the contact too, behind the shares-a-guild gate
		// (see peercache.go) — and rememberMembers re-runs it when membership
		// moves, which is what covers the peer whose invite is still in flight.
		go s.rememberPeer(p, pp.Fingerprint)
		// When a rendezvous node connects, register our mailbox with it and
		// drain anything deposited while we were offline.
		if s.isMailboxNode(p) {
			go func() {
				s.registerMailbox(p)
				s.drainMailbox(p)
			}()
		}
		// If this peer is an outstanding group-DM invitee we couldn't reach
		// earlier, invite them now that they're online.
		go s.deliverPendingDMInvites(p)
		go func() {
			time.Sleep(1500 * time.Millisecond)
			// Everything below is for a peer we have a relationship with. The
			// host also connects to the rendezvous, DHT routing peers and relay
			// candidates — several per 15s discovery tick — and running this
			// tail for each of those meant re-announcing our profile to every
			// guild (42 encrypted publishes on a real install), attempting a
			// history sync with someone who shares nothing, retrying it 10s
			// later, and probing stranded guilds. Per stranger. That is network
			// noise at best; at login, when discovery fires hardest, it is part
			// of why starting up dragged.
			//
			// The second look at 10s is not decoration: a member's linked device
			// can resolve to its own key for a moment after connecting, until
			// the roster commit carrying its certificate is applied. One recheck
			// on the existing retry beat covers that window without handing
			// every stranger a polling loop.
			known := s.knownContact(s.presence(p).Fingerprint)
			if known {
				s.announceProfileAll()
				// If we're in a call, say so now rather than on the next
				// heartbeat — see reannounceVoice.
				s.reannounceVoice()
			}
			// Pull any history we missed while apart from this peer; one retry
			// covers a stream that failed while the connection settled.
			if !known || !s.syncFromPeer(p) {
				time.Sleep(10 * time.Second)
				if !known {
					if known = s.knownContact(s.presence(p).Fingerprint); !known {
						return
					}
					s.announceProfileAll()
				}
				s.syncFromPeer(p)
			}
			// If sync couldn't bridge a gap and this peer can commit, this
			// newly-reachable committer is exactly who can re-add us.
			s.healStrandedGuilds()
		}()
	})

	host.OnPeerDisconnected(func(p peer.ID) {
		// A fresh connection deserves a fresh introduction, in both directions.
		s.greeted.release(p)
		s.answered.release(p)
		s.solicited.release(p)
		s.emitPeerDown(p, s.presence(p))
	})

	// A remembered address that no longer answers is worse than useless: it
	// costs a dial on every launch forever. Let the cache count the misses and
	// forget the peer once it's clearly gone.
	host.OnRedialFailed(func(p peer.ID) {
		s.peers.DialFailed(p.String())
		_ = s.peers.Flush(s.dataDir)
	})

	// Restore guilds we already belong to and re-subscribe to their topics.
	// MLS group state was reloaded from disk above, so a restarted member can
	// immediately decrypt and send at the epoch it left off.
	guilds, err := st.Guilds()
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("app: load guilds: %w", err)
	}
	for i := range guilds {
		s.trackGuild(&guilds[i])
	}

	// Restore DM lifecycle state (closed conversations, intended peers, pending
	// invites) now that the guild set is known, pruning entries for guilds that
	// no longer exist.
	s.loadDMState()
	s.loadBlocked()
	s.loadPendingMembers()
	s.loadMessageRequests()
	s.loadTypingPref()
	// Seed the per-contact device roster before anything can raise an alert, so
	// a restart doesn't report every device we already knew about as new.
	s.loadDeviceRoster()
	s.noteDeviceLeaves()
	// …and the devices of OUR OWN account, which no roster has to be readable
	// for us to know about (see devices.go). Revocations were loaded before any
	// of this, so nothing here can re-adopt a device we unlinked.
	s.loadOwnDevices()

	// Instant meetings are disposable — clear any that outlived their chosen
	// lifetime. Load the lifetimes FIRST, or the sweep judges every meeting by
	// the 24h default and deletes rooms whose links are good for a week.
	s.loadMeetingLife()
	s.sweepExpiredMeetings()

	// Browser guests: token validation + the relayed-session handler. Tokens are
	// restored here too: a link the host mailed out is meant to survive them
	// closing the app, which an in-memory-only token set could never do.
	s.initGuests()

	// Public booking page: availability config + the relayed slots/book
	// protocol. After initGuests — a booking answers with a guest link, and
	// after the meeting sweep so stale rooms are already gone.
	s.initBookings()

	// Guest-opened calendar events: restore which meeting rooms this node
	// hosts for its events. After the meeting sweep for the same reason as
	// bookings — records for rooms the sweep just deleted must be pruned.
	s.initEventGuests()

	// Drop contacts an older build recorded for every peer it happened to dial.
	// Once per launch is enough: recording is gated now, so the table can only
	// grow with people you actually share a group with.
	s.pruneContacts()

	// Background recovery: periodically re-attempt re-add for any stranded guild.
	go s.runHealLoop()

	// Keep this account's own devices reachable. Rendezvous discovery would get
	// there eventually; our own devices are the one set of peers we can look up
	// by name instead of searching for (see devices.go).
	go s.keepDevicesClose()

	// Resume rich presence if the user had it on.
	if s.RichPresenceEnabled() {
		s.startRichPresence()
	}

	return s, nil
}

// Fingerprint returns this peer's safety-number fingerprint.
func (s *Service) Fingerprint() string { return s.id.Fingerprint() }

// Mnemonic returns this identity's BIP39 recovery phrase (secret material).
func (s *Service) Mnemonic() (string, error) { return s.id.Mnemonic() }

// PeerID returns this peer's libp2p peer ID as a string.
func (s *Service) PeerID() string { return s.host.PeerID().String() }

// PublicKey returns this peer's Ed25519 account public key (its MLS credential).
func (s *Service) PublicKey() []byte { return []byte(s.id.PublicKey()) }

// Peers returns the currently connected peers.
func (s *Service) Peers() []PeerPresence {
	ids := s.host.Peers()
	out := make([]PeerPresence, 0, len(ids))
	for _, p := range ids {
		out = append(out, s.presence(p))
	}
	return out
}

// NetStatus is an aggregate view of connectivity for the UI's connection
// indicator. It's cheap to compute and safe to poll.
type NetStatus struct {
	Peers            int  `json:"peers"`            // total connected peers
	BootstrapReached bool `json:"bootstrapReached"` // at least one rendezvous node connected
	HasBootstrap     bool `json:"hasBootstrap"`     // any rendezvous node is configured
	OutOfSyncGuilds  int  `json:"outOfSyncGuilds"`  // guilds currently stranded (healing)
	// PinnedPortTaken: the fixed listen port was unavailable at startup and the
	// node fell back to an ephemeral one, so the user's router forward is dead
	// this session. Riding along here because it is the one status the settings
	// screen cannot work out for itself.
	PinnedPortTaken bool `json:"pinnedPortTaken"`
}

// NetworkStatus reports current connectivity for the UI banner: how many peers
// are connected, whether a rendezvous/relay node is reachable, and how many
// guilds are mid-heal. Mobile surfaces this as a connecting/online/offline pill.
func (s *Service) NetworkStatus() NetStatus {
	ns := NetStatus{
		Peers:           len(s.host.Peers()),
		HasBootstrap:    len(s.bootstrapPeers()) > 0,
		PinnedPortTaken: s.host.PinnedPortTaken(),
	}
	ns.BootstrapReached = len(s.mailboxNodes()) > 0
	s.mu.RLock()
	for _, g := range s.guilds {
		if s.outOfSync[g.ID] {
			ns.OutOfSyncGuilds++
		}
	}
	s.mu.RUnlock()
	return ns
}

// Nudge forces a fast reconnect + catch-up, called when the OS resumes the app
// (mobile) or the user hits "reconnect". It re-dials bootstrap nodes, re-drains
// the mailbox on any that are up, re-syncs history from every connected peer,
// and retries heal for stranded guilds. Safe to call repeatedly; each step is
// idempotent. Runs in the background so callers don't block on the network.
func (s *Service) Nudge() {
	go func() {
		// Re-dial configured rendezvous nodes we've lost.
		connected := map[peer.ID]bool{}
		for _, p := range s.host.Peers() {
			connected[p] = true
		}
		for _, pi := range s.bootstrapPeers() {
			if !connected[pi.ID] {
				ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
				_ = s.host.Connect(ctx, pi)
				cancel()
			}
		}
		// Drain the mailbox on every reachable rendezvous node.
		for _, node := range s.mailboxNodes() {
			s.registerMailbox(node)
			s.drainMailbox(node)
		}
		// Our own devices, by name rather than by search — the OS just told us
		// we were asleep, which is exactly when one of them has moved.
		s.reachOwnDevices()
		// Catch up history from every connected peer, then heal stragglers.
		for _, p := range s.host.Peers() {
			s.greet(p)
			s.syncFromPeer(p)
		}
		s.healStrandedGuilds()
	}()
}

// OnPeerConnected registers a presence-up callback.
//
// These two are the UI's presence feed, and they are gated on the peer being
// somebody the user actually has: the transport's connect/disconnect events are
// NOT. The host fires for every connection it makes — the rendezvous, the DHT
// routing peers it walks to find anyone, the relay candidates AutoRelay dials
// and drops, and with the public-DHT opt-in on, arbitrary IPFS nodes. Discovery
// re-runs on a 15s beat and dials everyone advertised under the rendezvous key,
// so on a rendezvous-configured install those arrive in bursts: measured, eight
// connect/disconnect transitions inside one second, none of them a person.
//
// That mattered because one presence event is not cheap upstairs. The UI treats
// it as "somebody's dot may have moved" and refetches the guild list, the member
// list, roles, contacts and its own profile, then re-renders the rail, the
// channel list and the member panel — which is why a burst read as the app
// refreshing over and over on its own, most visibly while switching between a DM
// and a server, when a re-render lands on top of a feed that is still loading.
//
// knownContact is the same predicate recordPeer uses to decide whether a
// connection is worth remembering (see peercache.go): verified, or we opened a
// conversation with them, or we share a guild. Anyone it rejects has no dot in
// this UI to move, so their connection is not presence. It costs a roster read
// per guild, which is what the callback's own recordPeer already spends, and it
// runs on the notifier's goroutine, not the network thread.
//
// A linked device whose certificate we haven't learned yet resolves to its own
// key and so reads as a stranger here. It used to STAY that way for the session
// unless unrelated traffic happened to carry the certificate past us. Now the
// device introduces itself over /concord/hello the moment it connects, and
// peerResolved fires this event after the fact (see hello.go) — which is why the
// callbacks live on the Service instead of being closed over inside the host
// hook, and why presenceShown exists to keep it to one up per connection.
func (s *Service) OnPeerConnected(fn func(PeerPresence)) {
	s.mu.Lock()
	s.onPeerUp = append(s.onPeerUp, fn)
	s.mu.Unlock()
}

// OnPeerDisconnected registers a presence-down callback. Gated exactly as
// OnPeerConnected is — a stranger going away is no more a presence change than
// a stranger arriving.
func (s *Service) OnPeerDisconnected(fn func(PeerPresence)) {
	s.mu.Lock()
	s.onPeerDown = append(s.onPeerDown, fn)
	s.mu.Unlock()
}

// emitPeerUp announces a peer's presence exactly once per connection.
func (s *Service) emitPeerUp(pp PeerPresence) {
	id, err := peer.Decode(pp.PeerID)
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.presenceShown == nil {
		s.presenceShown = map[peer.ID]bool{}
	}
	if s.presenceShown[id] {
		s.mu.Unlock()
		return
	}
	s.presenceShown[id] = true
	cbs := append([]func(PeerPresence){}, s.onPeerUp...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb(pp)
	}
}

// emitPeerDown announces a departure, but only for a peer whose arrival we
// announced — a down with no matching up is a phantom in the UI's feed.
func (s *Service) emitPeerDown(p peer.ID, pp PeerPresence) {
	s.mu.Lock()
	shown := s.presenceShown[p]
	delete(s.presenceShown, p)
	cbs := append([]func(PeerPresence){}, s.onPeerDown...)
	s.mu.Unlock()
	if !shown {
		return
	}
	for _, cb := range cbs {
		cb(pp)
	}
}

// OnMessage registers a callback fired for every message — sent or received —
// after it has been persisted. This is the UI's live message feed.
func (s *Service) OnMessage(fn func(domain.Message)) {
	s.mu.Lock()
	s.onMessage = append(s.onMessage, fn)
	s.mu.Unlock()
}

func (s *Service) emitMessage(m domain.Message) {
	s.mu.RLock()
	cbs := append([]func(domain.Message){}, s.onMessage...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(m)
	}
}

// SetBootstrapLive replaces the saved rendezvous list and dials the new nodes
// immediately (best-effort), so an in-app change helps the current session too.
// Full DHT re-init still happens on next launch.
func (s *Service) SetBootstrapLive(addrs []string) error {
	if err := SaveBootstrap(s.dataDir, addrs); err != nil {
		return err
	}
	if infos, err := parseBootstrapPeers(addrs); err == nil {
		// Adopt them for THIS session as well, not just on disk. Without this the
		// address is saved and dialled but s.bootstrap still holds the old set,
		// so for the rest of the session the mailbox never registers with the new
		// rendezvous, Nudge() won't re-dial it, and the diagnostics panel reports
		// "not configured" while an open connection to that very node is sitting
		// in the peer list — which reads, correctly, as "the rendezvous is
		// broken".
		s.setBootstrapPeers(infos)
		// Relay candidates too, or AutoRelay keeps offering the rendezvous the
		// user just replaced until the process restarts.
		s.host.SetBootstrapPeers(infos)
		for _, pi := range infos {
			pi := pi
			go func() { _ = s.host.Connect(s.ctx, pi) }()
		}
	}
	return nil
}

// bootstrapPeers returns the current rendezvous set. The slice is copied: the
// caller iterates it without the lock, and SetBootstrapLive can replace the
// field underneath them at any moment.
func (s *Service) bootstrapPeers() []peer.AddrInfo {
	s.bootstrapMu.RLock()
	defer s.bootstrapMu.RUnlock()
	if len(s.bootstrap) == 0 {
		return nil
	}
	out := make([]peer.AddrInfo, len(s.bootstrap))
	copy(out, s.bootstrap)
	return out
}

func (s *Service) setBootstrapPeers(infos []peer.AddrInfo) {
	s.bootstrapMu.Lock()
	s.bootstrap = infos
	s.bootstrapMu.Unlock()
}

// DisplayName returns this peer's chosen display name, defaulting to the first
// block of its fingerprint when unset.
func (s *Service) DisplayName() string {
	name, _ := s.store.GetSetting("display_name")
	if strings.TrimSpace(name) != "" {
		return name
	}
	fpr := s.id.Fingerprint()
	if i := strings.IndexByte(fpr, ' '); i > 0 {
		return fpr[:i]
	}
	return fpr
}

// SetDisplayName persists a display name (self-asserted; the fingerprint remains
// the authenticated identity) and re-announces it to every guild.
func (s *Service) SetDisplayName(name string) error {
	if err := s.store.SetSetting("display_name", strings.TrimSpace(name)); err != nil {
		return err
	}
	s.bumpProfileStamp()
	s.announceProfileAll()
	s.regreetOwnDevices()
	return nil
}

// SelfProfile returns this peer's own profile.
func (s *Service) SelfProfile() Profile {
	status, _ := s.store.GetSetting("status_text")
	emoji, _ := s.store.GetSetting("avatar_emoji")
	color, _ := s.store.GetSetting("accent_color")
	avatar, _ := s.store.GetSetting("avatar_image")
	banner, _ := s.store.GetSetting("banner_image")
	presence, _ := s.store.GetSetting("presence")
	bio, _ := s.store.GetSetting("bio")
	// Rich presence travels as structured Activity alongside the manual status
	// — it does NOT replace a status the user chose. The 🎵 string only stands
	// in when there's no manual status (also what pre-activity clients show).
	s.activityMu.Lock()
	var act *Activity
	if s.activity != "" {
		if status == "" {
			status = s.activity
		}
		act = s.activityInfo
	}
	s.activityMu.Unlock()
	rawGames, _ := s.store.GetSetting("games")
	color2, _ := s.store.GetSetting("accent_color2")
	frame, _ := s.store.GetSetting("avatar_frame")
	effect, _ := s.store.GetSetting("card_effect")
	var style *Style
	if raw, _ := s.store.GetSetting("card_style"); raw != "" {
		var st Style
		if json.Unmarshal([]byte(raw), &st) == nil {
			style = sanitizeStyle(&st)
		}
	}
	return Profile{
		Name: s.DisplayName(), Status: status, Emoji: emoji, Color: color, Avatar: avatar,
		Banner: banner, Presence: presence, Bio: bio, MailboxPub: s.mbxPub[:], Activity: act,
		Games: decodeGames(rawGames), Color2: color2, Frame: frame, Effect: effect, Style: style,
		UpdatedAt: s.profileStamp(),
	}
}

// selfStoredProfile is the profile as STORED, for the account's own devices:
// link handover, device hello, sync roster. It differs from SelfProfile in one
// deliberate way — no rich-presence substitution. SelfProfile stands the 🎵
// now-playing line in for an empty status because that's what peers should
// SEE; writing that presentation copy into another device's settings would
// make a passing song a permanent manual status.
func (s *Service) selfStoredProfile() Profile {
	p := s.SelfProfile()
	p.Activity = nil
	p.Status, _ = s.store.GetSetting("status_text")
	return p
}

// profileStamp reads this account's profile edit stamp (UnixMilli, 0 = a
// profile that predates stamps or was never edited).
func (s *Service) profileStamp() int64 {
	raw, _ := s.store.GetSetting("profile_updated_at")
	if raw == "" {
		return 0
	}
	at, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return at
}

// bumpProfileStamp advances the edit stamp to now, strictly past the previous
// value — two edits inside one clock tick (or across skewed device clocks)
// must never tie, or the second edit loses last-writer-wins.
func (s *Service) bumpProfileStamp() int64 {
	at := time.Now().UnixMilli()
	if prev := s.profileStamp(); at <= prev {
		at = prev + 1
	}
	_ = s.store.SetSetting("profile_updated_at", strconv.FormatInt(at, 10))
	return at
}

// AdoptLinkedProfile applies a profile edit authored by ANOTHER device of this
// very account (link handover, device hello, sync roster — the caller has
// already authenticated the author as this account). Last-writer-wins on the
// edit stamp: an older or equal copy changes nothing, which is also what stops
// two devices greeting each other from ping-ponging the same profile forever.
// Reports whether anything was adopted.
func (s *Service) AdoptLinkedProfile(p Profile) bool {
	if p.UpdatedAt <= s.profileStamp() {
		return false
	}
	// Bound every field exactly as a local edit would be. The author is our own
	// account, but a bug (or compromise) on one device must not be able to wedge
	// every other device with an oversized or malformed field.
	if p.Avatar != "" && !validImageDataURI(p.Avatar, maxAvatarBytes) {
		p.Avatar = ""
	}
	if len(p.Banner) > maxProfileBannerBytes || !validBanner(p.Banner) {
		p.Banner = ""
	}
	if len(p.Bio) > maxBioBytes {
		p.Bio = p.Bio[:maxBioBytes]
	}
	sanitizeProfileExtras(&p)
	gamesJSON := ""
	if games := sanitizeGames(p.Games); len(games) > 0 {
		if raw, err := json.Marshal(games); err == nil {
			gamesJSON = string(raw)
		}
	}
	for k, v := range map[string]string{
		"display_name":  strings.TrimSpace(p.Name),
		"status_text":   strings.TrimSpace(p.Status),
		"avatar_emoji":  strings.TrimSpace(p.Emoji),
		"accent_color":  strings.TrimSpace(p.Color),
		"avatar_image":  p.Avatar,
		"banner_image":  p.Banner,
		"presence":      strings.TrimSpace(p.Presence),
		"bio":           strings.TrimSpace(p.Bio),
		"accent_color2": p.Color2,
		"avatar_frame":  p.Frame,
		"card_effect":   p.Effect,
		"card_style":    encodeStyle(p.Style),
		"games":         gamesJSON,
	} {
		if err := s.store.SetSetting(k, v); err != nil {
			return false
		}
	}
	// Keep the AUTHOR's stamp, not a fresh one: re-stamping would make this
	// device look like the newest editor and reflect the same profile back.
	_ = s.store.SetSetting("profile_updated_at", strconv.FormatInt(p.UpdatedAt, 10))
	s.emitGuildUpdate()
	// Our guilds' members may only be hearing from THIS device — tell them too.
	s.announceProfileAll()
	return true
}

// SetGames replaces this peer's game collection and re-announces the profile
// to every guild so members' profile cards update.
func (s *Service) SetGames(games []Game) error {
	raw, err := json.Marshal(sanitizeGames(games))
	if err != nil {
		return err
	}
	if err := s.store.SetSetting("games", string(raw)); err != nil {
		return err
	}
	s.bumpProfileStamp()
	s.announceProfileAll()
	s.regreetOwnDevices()
	return nil
}

// SetProfile persists the full self profile and re-announces it to every guild.
func (s *Service) SetProfile(p Profile) error {
	if len(p.Avatar) > maxAvatarBytes {
		return fmt.Errorf("app: avatar image too large (max %d KB)", maxAvatarBytes/1024)
	}
	if p.Avatar != "" && !strings.HasPrefix(p.Avatar, "data:image/") {
		return fmt.Errorf("app: avatar must be an image data URI")
	}
	if len(p.Banner) > maxProfileBannerBytes {
		return fmt.Errorf("app: banner image too large (max %d KB)", maxProfileBannerBytes/1024)
	}
	if !validBanner(p.Banner) {
		return fmt.Errorf("app: banner must be an image or a preset")
	}
	if len(p.Bio) > maxBioBytes {
		p.Bio = p.Bio[:maxBioBytes]
	}
	sanitizeProfileExtras(&p)
	for k, v := range map[string]string{
		"display_name":  strings.TrimSpace(p.Name),
		"status_text":   strings.TrimSpace(p.Status),
		"avatar_emoji":  strings.TrimSpace(p.Emoji),
		"accent_color":  strings.TrimSpace(p.Color),
		"avatar_image":  p.Avatar,
		"banner_image":  p.Banner,
		"presence":      strings.TrimSpace(p.Presence),
		"bio":           strings.TrimSpace(p.Bio),
		"accent_color2": p.Color2,
		"avatar_frame":  p.Frame,
		"card_effect":   p.Effect,
		"card_style":    encodeStyle(p.Style),
	} {
		if err := s.store.SetSetting(k, v); err != nil {
			return err
		}
	}
	s.bumpProfileStamp()
	s.announceProfileAll()
	// The gossip announce above reaches PEERS; our own devices adopt the raw
	// stored profile over the device hello instead (see selfStoredProfile for
	// why the two copies differ), so push a fresh hello at them now.
	s.regreetOwnDevices()
	return nil
}

// ProfileOf returns a peer's learned profile for a fingerprint (zero if unknown).
func (s *Service) ProfileOf(fingerprint string) Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profiles[fingerprint]
}

// NickOf returns the per-guild nickname for a member, or "" if none is set.
func (s *Service) NickOf(guildID, fingerprint string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if g := s.nicks[guildID]; g != nil {
		return g[fingerprint]
	}
	return ""
}

// rememberNick records (or clears, on empty nick) a per-guild nickname in the
// in-memory cache and persists it. Caller holds no lock.
func (s *Service) rememberNick(guildID, fingerprint, nick string) {
	s.mu.Lock()
	if nick == "" {
		if g := s.nicks[guildID]; g != nil {
			delete(g, fingerprint)
		}
	} else {
		if s.nicks[guildID] == nil {
			s.nicks[guildID] = map[string]string{}
		}
		s.nicks[guildID][fingerprint] = nick
	}
	s.mu.Unlock()
	_ = s.store.SaveNickname(guildID, fingerprint, nick)
}

// learnProfile validates, records, persists, and surfaces a peer's self-asserted
// profile. It is the single sink for every way a profile can arrive (gossip
// announce, invite handshake, history sync). Reports whether the fingerprint
// was previously unknown.
func (s *Service) learnProfile(fingerprint string, p Profile) bool {
	if fingerprint == "" || fingerprint == s.id.Fingerprint() {
		return false
	}
	if p.Avatar != "" && !validImageDataURI(p.Avatar, maxAvatarBytes) {
		p.Avatar = "" // reject oversized / malformed / non-image avatars from peers
	}
	if len(p.Banner) > maxProfileBannerBytes || !validBanner(p.Banner) {
		p.Banner = "" // reject oversized / malformed banners from peers
	}
	if a := p.Activity; a != nil {
		// Peers only get to broadcast plausible activity: web art URLs (no
		// file:///javascript: junk that a client might render), bounded sizes.
		if len(a.Title) > maxActivityBytes || len(a.Artist) > maxActivityBytes {
			p.Activity = nil
		} else if !validArtURL(a.ArtURL) {
			a.ArtURL = ""
		}
	}
	p.Games = sanitizeGames(p.Games) // bound peers' collections like our own
	sanitizeProfileExtras(&p)        // and their decorative extras
	// Don't let a partial update wipe fields we already learned. Peers relay each
	// other's profiles over the sync roster, and a peer that only knows someone
	// as "unknown" (empty name) would otherwise blank a good name — which the UI
	// then shows as a fingerprint stub, causing the name to flicker. An empty
	// name is never an intentional clear (users always have a display name), so
	// keep the previous one; likewise keep a known mailbox key.
	s.mu.Lock()
	prev, known := s.profiles[fingerprint]
	// Last-writer-wins when both copies carry an edit stamp: a peer relaying a
	// STALE copy (an old roster, a device that slept through the edit) must not
	// roll back a newer one. Equal stamps still apply — activity/now-playing
	// updates ride announces without bumping the stamp. A stampless legacy copy
	// (0) keeps the old newest-arrival-wins behavior.
	if known && p.UpdatedAt != 0 && prev.UpdatedAt != 0 && p.UpdatedAt < prev.UpdatedAt {
		s.mu.Unlock()
		return false
	}
	if known {
		if p.Name == "" && prev.Name != "" {
			p.Name = prev.Name
		}
		if len(p.MailboxPub) == 0 && len(prev.MailboxPub) > 0 {
			p.MailboxPub = prev.MailboxPub
		}
	}
	s.profiles[fingerprint] = p
	s.mu.Unlock()
	if known && profilesEqual(prev, p) {
		return false // nothing new; skip the store write and UI refresh
	}
	gamesJSON := ""
	if len(p.Games) > 0 {
		if raw, err := json.Marshal(p.Games); err == nil {
			gamesJSON = string(raw)
		}
	}
	_ = s.store.SaveProfile(store.ProfileRow{
		Fingerprint: fingerprint,
		Name:        p.Name, Status: p.Status, Emoji: p.Emoji, Color: p.Color, Avatar: p.Avatar,
		Banner: p.Banner, Presence: p.Presence, Bio: p.Bio, MailboxPub: p.MailboxPub,
		Games: gamesJSON, Color2: p.Color2, Frame: p.Frame, Effect: p.Effect, Style: encodeStyle(p.Style),
	})
	s.emitGuildUpdate()
	return !known
}

func profilesEqual(a, b Profile) bool {
	return a.Name == b.Name && a.Status == b.Status && a.Emoji == b.Emoji &&
		a.Color == b.Color && a.Avatar == b.Avatar && a.Banner == b.Banner &&
		a.Presence == b.Presence && a.Bio == b.Bio && bytes.Equal(a.MailboxPub, b.MailboxPub) &&
		a.Color2 == b.Color2 && a.Frame == b.Frame && a.Effect == b.Effect &&
		stylesEqual(a.Style, b.Style) &&
		activityEqual(a.Activity, b.Activity) && activityPosEqual(a.Activity, b.Activity) &&
		gamesEqual(a.Games, b.Games)
}

func gamesEqual(a, b []Game) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// activityPosEqual detects re-announced position snapshots (seeks) so they
// reach the UI even when the track itself didn't change.
func activityPosEqual(a, b *Activity) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.PositionMs == b.PositionMs && a.AtMs == b.AtMs
}

// learnNameHint backfills a display name from a chat message's self-asserted
// name ONLY when we have no learned name for that member yet. This keeps the
// chat and the member list consistent: without it, the roster (learned
// profile) can show a fingerprint code while the message shows a name, or vice
// versa. It never overwrites a richer learned profile (emoji/color/avatar) and
// never replaces a name we already have.
func (s *Service) learnNameHint(fingerprint, name string) {
	name = strings.TrimSpace(name)
	if fingerprint == "" || name == "" || fingerprint == s.id.Fingerprint() {
		return
	}
	s.mu.RLock()
	cur, known := s.profiles[fingerprint]
	s.mu.RUnlock()
	if known && cur.Name != "" {
		return // already have a name; don't clobber it with a stale message's
	}
	p := cur
	p.Name = name
	s.learnProfile(fingerprint, p)
}

// OutOfSync reports whether a guild is stranded at an old MLS epoch that no
// reachable peer's commit log could bridge (the member needs a re-invite).
func (s *Service) OutOfSync(guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.outOfSync[guildID]
}

// setOutOfSync flips a guild's stranded flag, refreshing the UI on transitions.
func (s *Service) setOutOfSync(guildID string, stranded bool) {
	s.mu.Lock()
	was := s.outOfSync[guildID]
	if stranded {
		s.outOfSync[guildID] = true
	} else {
		delete(s.outOfSync, guildID)
	}
	s.mu.Unlock()
	if was != stranded {
		s.emitGuildUpdate()
	}
	// Newly stranded: try to auto-recover immediately — history-sync from any
	// member first, a committer re-add only if nobody can bridge us (see
	// recoverOutOfSync). The heal loop retries if no one is reachable yet.
	if stranded && !was {
		go s.recoverOutOfSync(guildID)
	}
}

// ProfileName returns a peer's learned display name for a fingerprint, or "".
func (s *Service) ProfileName(fingerprint string) string {
	return s.ProfileOf(fingerprint).Name
}

// Contacts returns known peers and their verification status.
// GuildInvite is an offer to join a server, pushed by a contact you verified.
// It is NOT membership: nothing happens until the invitee accepts and redeems
// the code themselves.
type GuildInvite struct {
	Code     string `json:"code"`
	Guild    string `json:"guild"`
	From     string `json:"from"`     // inviter's fingerprint
	FromName string `json:"fromName"` // inviter's display name
}

// OnGuildInvite fires when a verified contact offers to add us to a server.
func (s *Service) OnGuildInvite(fn func(GuildInvite)) {
	s.mu.Lock()
	s.onGuildInvite = append(s.onGuildInvite, fn)
	s.mu.Unlock()
}

func (s *Service) emitGuildInvite(inv GuildInvite) {
	s.mu.RLock()
	cbs := append([]func(GuildInvite){}, s.onGuildInvite...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(inv)
	}
}

func (s *Service) Contacts() ([]domain.Contact, error) {
	return s.store.Contacts()
}

// VerifyContact marks a peer as human-verified after an out-of-band fingerprint
// check.
func (s *Service) VerifyContact(peerID string) error {
	return s.store.SetVerified(peerID)
}

// VerifyFingerprint marks a fingerprint (a member's stable identity) as
// human-verified after an out-of-band comparison.
//
// Verifying must never depend on a contact row already existing. The underlying
// call is an UPDATE, so it failed — with a raw store string shown straight to the
// user — for anyone whose row we had not recorded or had since pruned: a guild
// member you have never had a direct connection to, or a contact the launch-time
// prune took. Fall back to the same placeholder-row insert device linking uses;
// the user comparing safety numbers is the whole of the evidence here, and it
// does not become less true because we have no row.
func (s *Service) VerifyFingerprint(fingerprint string) error {
	if fingerprint == "" {
		return fmt.Errorf("app: no fingerprint given")
	}
	if err := s.store.SetVerifiedByFingerprint(fingerprint); err == nil {
		return nil
	}
	return s.store.ImportVerifiedFingerprint(fingerprint)
}

// ImportVerifiedFingerprints seeds verifications carried over from another
// device (device linking). Unlike VerifyFingerprint it doesn't require the
// peers to have been sighted on this device yet. Best-effort per entry.
func (s *Service) ImportVerifiedFingerprints(fprs []string) {
	for _, f := range fprs {
		_ = s.store.ImportVerifiedFingerprint(f)
	}
}

// VerifiedFingerprints returns which fingerprints the user has verified.
func (s *Service) VerifiedFingerprints() map[string]bool {
	m, err := s.store.VerifiedFingerprints()
	if err != nil {
		return map[string]bool{}
	}
	return m
}

// Close shuts everything down.
func (s *Service) Close() error {
	if s.peers != nil {
		// Flush past the write throttle: everything learned in the last few
		// seconds of a session is exactly what the next launch wants.
		_ = s.peers.Save(s.dataDir)
	}
	if s.host != nil {
		_ = s.host.Close()
	}
	if s.mls != nil {
		_ = s.mls.Close()
	}
	if s.store != nil {
		_ = s.store.Close()
	}
	return nil
}

// VerifyPassphrase reports whether passphrase decrypts the keystore in dataDir.
// Used to re-check the passphrase when a session is already unlocked, so a
// second unlock attempt can't silently succeed with the wrong passphrase.
func VerifyPassphrase(dataDir, passphrase string) bool {
	_, err := identity.LoadKeystore(keystorePathIn(dataDir), passphrase)
	return err == nil
}

// parseBootstrapPeers converts multiaddr strings (each ending in /p2p/<id>) to
// AddrInfos for DHT bootstrapping and relay.
func parseBootstrapPeers(addrs []string) ([]peer.AddrInfo, error) {
	var out []peer.AddrInfo
	for _, a := range addrs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			return nil, fmt.Errorf("app: bad bootstrap addr %q: %w", a, err)
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return nil, fmt.Errorf("app: bad bootstrap addr %q: %w", a, err)
		}
		out = append(out, *pi)
	}
	return out, nil
}

// deriveStoreKey derives the 32-byte at-rest message key from the identity seed
// via HKDF, so message-body encryption is bound to the passphrase-protected
// identity without a second secret.
func deriveStoreKey(id *identity.Identity) []byte {
	r := hkdf.New(sha256.New, id.Seed(), nil, []byte("concord-store-v1"))
	key := make([]byte, 32)
	_, _ = io.ReadFull(r, key)
	return key
}

// deriveMLSSigningKey derives a dedicated, deterministic Ed25519 key for signing
// MLS messages. It is HKDF-separated from the identity's libp2p key so the two
// protocols never share signing material, yet it is reproducible on every start
// (no storage needed) — which is what makes send-after-restart work.
func deriveMLSSigningKey(id *identity.Identity) ed25519.PrivateKey {
	r := hkdf.New(sha256.New, id.Seed(), nil, []byte("concord-mls-sig-v1"))
	seed := make([]byte, ed25519.SeedSize)
	_, _ = io.ReadFull(r, seed)
	return ed25519.NewKeyFromSeed(seed)
}

// accountKeyOf returns the account public key that a member credential
// authenticates under — the single identity that ownership, bans, roles, and
// message attribution are all keyed on, regardless of which device produced the
// leaf. A legacy bare 32-byte credential IS the account key (single-device, and
// every pre-multi-device client), so it passes straight through — this keeps the
// whole authorization model unchanged for existing guilds. A device-cert
// credential (multi-device) carries its accountPub and is only honored when the
// account's signature over the device key verifies; an unverified/garbage cert
// returns the raw bytes so a forged cert can't silently masquerade as some other
// account (it just fails to match any real member).
func accountKeyOf(cred []byte) []byte {
	if cert, ok := identity.ParseDeviceCert(cred); ok {
		if cert.Verify() {
			return cert.AccountPub
		}
		return cred // invalid cert: don't let it resolve to a claimed account
	}
	return cred
}

// accountFingerprintOf is the safety-number fingerprint of the account behind a
// credential — use this instead of FingerprintOf(cred) everywhere a credential
// is turned into a member identity, so device leaves map to their account.
func accountFingerprintOf(cred []byte) string {
	return identity.FingerprintOf(accountKeyOf(cred))
}

// AccountKeyOf / AccountFingerprintOf expose credential normalization to the
// bridge, so a member view collapses all of an account's device leaves onto the
// single account identity (one row, the account's name, not a per-device cert).
func (s *Service) AccountKeyOf(cred []byte) []byte         { return accountKeyOf(cred) }
func (s *Service) AccountFingerprintOf(cred []byte) string { return accountFingerprintOf(cred) }

// presenceFor derives the UI view for a peer from the key embedded in its
// PeerID. This is correct for a legacy account-key PeerID (PeerID == account);
// a linked device's PeerID is its device key, which this can't map to an
// account — use Service.presence for that.
func presenceFor(p peer.ID) PeerPresence {
	pp := PeerPresence{PeerID: p.String()}
	if pub, err := p.ExtractPublicKey(); err == nil {
		if raw, err := pub.Raw(); err == nil {
			pp.Fingerprint = identity.FingerprintOf(raw)
		}
	}
	return pp
}

// learnDeviceCert records the device→account mapping from a credential, so a
// later presence() lookup for that device's PeerID resolves to the account.
// A no-op for a legacy bare credential (PeerID already == account).
func (s *Service) learnDeviceCert(cred []byte) {
	cert, ok := identity.ParseDeviceCert(cred)
	if !ok || !cert.Verify() {
		return
	}
	// A device of ours that we unlinked must not be re-adopted, whatever carries
	// its certificate back to us — a leaf we could not remove from some guild, a
	// message that was already in flight, its own hello. This is the single place
	// every one of those paths goes through.
	if s.deviceIsRevoked(cert.DevicePub) {
		return
	}
	key := hex.EncodeToString(cert.DevicePub)
	fpr := identity.FingerprintOf(cert.AccountPub)
	s.deviceMu.Lock()
	if s.deviceAccounts == nil {
		s.deviceAccounts = map[string]string{}
	}
	s.deviceAccounts[key] = fpr
	s.deviceMu.Unlock()
}

// relearnDevices rebuilds the device→account map from a guild's MLS roster.
//
// The map is otherwise only written when we serve someone's join or decrypt a
// message they sent, and it lives in memory. So it emptied on every restart,
// and a linked device that had been quiet since then resolved to the
// fingerprint of its own device key — which matches no member. Everything that
// asks "are you in this guild?" then said no: history sync answered its
// catch-up requests with zero bytes, and because that reply is indistinguishable
// from "nothing to send", the device retried every twenty seconds forever
// without one error to show for it. A phone that missed a commit while asleep
// could never come back.
//
// The roster is the authority we already trust for membership, and every leaf
// carries the device cert that states the mapping, so reading it back is enough.
func (s *Service) relearnDevices(groupID []byte) {
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return
	}
	for _, c := range creds {
		s.learnDeviceCert(c)
	}
}

// lookupDevice returns the account fingerprint learned for a device key, or ""
// when we have never seen a cert for it.
func (s *Service) lookupDevice(devicePub []byte) string {
	s.deviceMu.RLock()
	defer s.deviceMu.RUnlock()
	return s.deviceAccounts[hex.EncodeToString(devicePub)]
}

// presence is the account-aware presenceFor: it resolves a linked device's
// PeerID (its device key) to the account fingerprint via the learned map,
// falling back to the raw key's fingerprint for a legacy account-key PeerID.
func (s *Service) presence(p peer.ID) PeerPresence {
	pp := PeerPresence{PeerID: p.String()}
	pub, err := p.ExtractPublicKey()
	if err != nil {
		return pp
	}
	raw, err := pub.Raw()
	if err != nil {
		return pp
	}
	s.deviceMu.RLock()
	fpr, isDevice := s.deviceAccounts[hex.EncodeToString(raw)]
	s.deviceMu.RUnlock()
	if isDevice {
		pp.Fingerprint = fpr
	} else {
		pp.Fingerprint = identity.FingerprintOf(raw)
	}
	return pp
}
