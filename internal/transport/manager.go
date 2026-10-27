package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Settings is what the user chose, persisted next to the rest of the data dir.
// Every link starts off: turning on a radio is the user's call.
type Settings struct {
	Enabled map[string]bool `json:"enabled"`
	// Offline restricts Concord's extra links to Local ones — no satellite, no
	// paid hop — and stops the others.
	Offline bool `json:"offline"`
	// Relay lets this device carry other people's ciphertext a few hops on its
	// live local links, which is what turns a room of phones into a mesh. It
	// only ever forwards bytes it cannot read.
	Relay bool `json:"relay"`
}

// LinkView is one row of the settings page.
type LinkView struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Caps    Caps   `json:"caps"`
	Status  Status `json:"status"`
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	// Queued frames waiting in the outbox of a Manual or paid link, and what
	// sending all of them would cost.
	Queued     int    `json:"queued"`
	QueuedCost int64  `json:"queuedCostMicroUSD"`
	Sent       int    `json:"sent"`
	Received   int    `json:"received"`
	Error      string `json:"error,omitempty"`
}

const (
	outboxCap = 512 // frames held per manual link before the oldest go
	seenCap   = 8192
	// liveAirtime is the longest a Live link may spend on one message before
	// the Manager stops considering it for automatic sends — a 40-second LoRa
	// burst for a chat line starves everyone else on the channel.
	liveAirtime = 60 * time.Second
	// relayPerMinute bounds how much of other people's traffic this device
	// will put on the air.
	relayPerMinute = 30
)

type link struct {
	t        Transport
	running  bool
	cancel   context.CancelFunc
	outbox   [][]byte
	sent     int
	received int
	err      string
}

// Manager owns every registered link and decides what goes where.
type Manager struct {
	mu       sync.Mutex
	ctx      context.Context
	path     string
	settings Settings
	links    map[string]*link
	topics   map[string]bool
	tags     map[uint16]map[[8]byte]string // day → tag → topic, rebuilt lazily
	seen     *seenSet
	reasm    *Reassembler
	deliver  func(topic string, payload []byte)
	relayed  []time.Time
	now      func() time.Time
}

// NewManager loads settings from path (a missing file means everything off)
// and hands every message that arrives for a carried topic to deliver.
func NewManager(path string, deliver func(topic string, payload []byte)) *Manager {
	m := &Manager{
		ctx:     context.Background(),
		path:    path,
		links:   map[string]*link{},
		topics:  map[string]bool{},
		tags:    map[uint16]map[[8]byte]string{},
		seen:    newSeenSet(seenCap),
		reasm:   NewReassembler(64, 30*time.Minute),
		deliver: deliver,
		now:     time.Now,
	}
	m.settings = Settings{Enabled: map[string]bool{}, Relay: true}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var s Settings
			if json.Unmarshal(raw, &s) == nil {
				if s.Enabled == nil {
					s.Enabled = map[string]bool{}
				}
				m.settings = s
			}
		}
	}
	return m
}

// SetDeliver sets where arriving messages for carried topics go. Set it
// before Start.
func (m *Manager) SetDeliver(deliver func(topic string, payload []byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliver = deliver
}

// Register adds a link. It is not started until Start, and then only if the
// user enabled it.
func (m *Manager) Register(t Transport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[t.Name()] = &link{t: t}
}

// Start brings up every enabled link and keeps ctx as their lifetime.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	names := m.sortedNames()
	m.mu.Unlock()
	for _, n := range names {
		m.reconcile(n)
	}
}

// Close stops every running link.
func (m *Manager) Close() {
	m.mu.Lock()
	names := m.sortedNames()
	m.mu.Unlock()
	for _, n := range names {
		m.stop(n)
	}
}

// Settings returns a copy of the current choices.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	s.Enabled = make(map[string]bool, len(m.settings.Enabled))
	for k, v := range m.settings.Enabled {
		s.Enabled[k] = v
	}
	return s
}

// SetEnabled turns one link on or off and persists the choice.
func (m *Manager) SetEnabled(name string, on bool) error {
	m.mu.Lock()
	if m.links[name] == nil {
		m.mu.Unlock()
		return fmt.Errorf("transport: no link %q", name)
	}
	m.settings.Enabled[name] = on
	err := m.saveLocked()
	m.mu.Unlock()
	m.reconcile(name)
	return err
}

// SetOffline switches offline mode, which keeps only Local links running.
func (m *Manager) SetOffline(on bool) error {
	m.mu.Lock()
	m.settings.Offline = on
	err := m.saveLocked()
	names := m.sortedNames()
	m.mu.Unlock()
	for _, n := range names {
		m.reconcile(n)
	}
	return err
}

// SetRelay switches whether this device forwards other people's bundles.
func (m *Manager) SetRelay(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings.Relay = on
	return m.saveLocked()
}

// Carry marks a topic as one whose messages may travel over these links, and
// whose arrivals here should be delivered. Typing and voice signalling are not
// carried: they are presence hints with no value a minute later, and they are
// exactly the chatter that would drown a 200-byte radio.
func (m *Manager) Carry(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.topics[topic] {
		m.topics[topic] = true
		m.tags = map[uint16]map[[8]byte]string{}
	}
}

// Drop stops carrying a topic.
func (m *Manager) Drop(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.topics[topic] {
		delete(m.topics, topic)
		m.tags = map[uint16]map[[8]byte]string{}
	}
}

// Carries reports whether a topic is carried.
func (m *Manager) Carries(topic string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.topics[topic]
}

// Observe records a payload as seen and reports whether it was new. The
// gossipsub path calls it for every message it delivers, so a copy that also
// comes in over Bluetooth a second later is recognised and dropped.
func (m *Manager) Observe(payload []byte) bool {
	return m.seen.add(ID(payload))
}

// Send offers one ciphertext for a carried topic to every eligible link.
// Live free links transmit now; Manual and paid links queue it for the user.
// It never blocks on a slow radio: transmission happens in the background.
func (m *Manager) Send(topic string, payload []byte) {
	m.seen.add(ID(payload)) // our own copy echoing back is not news
	m.mu.Lock()
	if !m.topics[topic] {
		m.mu.Unlock()
		return
	}
	day := Today(m.now())
	b := Bundle{Hops: DefaultHops, Day: day, Tag: TopicTag(topic, day), Payload: payload}
	m.mu.Unlock()
	m.dispatch(b, "")
}

// dispatch routes a bundle to links; skip names the link a relayed bundle
// came in on when that link is point-to-point (unused for broadcast media).
func (m *Manager) dispatch(b Bundle, skip string) {
	m.mu.Lock()
	type job struct {
		name   string
		frames [][]byte
	}
	var jobs []job
	for _, name := range m.sortedNames() {
		l := m.links[name]
		if name == skip || !l.running {
			continue
		}
		caps := l.t.Caps()
		if !m.eligibleLocked(caps, len(b.Payload)) {
			continue
		}
		frames, err := Frames(b, caps.MTU)
		if err != nil {
			continue
		}
		if caps.Mode == Manual || caps.Paid() {
			l.outbox = append(l.outbox, frames...)
			if over := len(l.outbox) - outboxCap; over > 0 {
				l.outbox = l.outbox[over:]
			}
			continue
		}
		jobs = append(jobs, job{name, frames})
	}
	ctx := m.ctx
	m.mu.Unlock()
	for _, j := range jobs {
		go func() { _, _ = m.transmit(ctx, j.name, j.frames) }()
	}
}

// eligibleLocked is the auto-pick: a link carries a message only if ciphertext
// is lawful on it, offline mode permits it, and it can move the message in a
// reasonable time. Cost does not exclude a link here — it moves it to the
// outbox, where the user sees the price before anything is spent.
func (m *Manager) eligibleLocked(c Caps, n int) bool {
	if !c.Encryption {
		return false
	}
	if m.settings.Offline && !c.Local {
		return false
	}
	frames := FrameCount(n, c.MTU)
	if frames == 0 {
		return false
	}
	if c.Mode == Live && !c.Paid() && c.Bitrate > 0 {
		airtime := time.Duration(frames*c.MTU*8) * time.Second / time.Duration(c.Bitrate)
		if airtime > liveAirtime {
			return false
		}
	}
	return true
}

func (m *Manager) transmit(ctx context.Context, name string, frames [][]byte) (int, error) {
	m.mu.Lock()
	l := m.links[name]
	m.mu.Unlock()
	if l == nil {
		return 0, fmt.Errorf("transport: no link %q", name)
	}
	for i, f := range frames {
		if err := l.t.Send(ctx, f); err != nil {
			m.mu.Lock()
			l.err = err.Error()
			m.mu.Unlock()
			log.Printf("transport: %s send: %v", name, err)
			return i, err
		}
		m.mu.Lock()
		l.sent++
		l.err = ""
		m.mu.Unlock()
	}
	return len(frames), nil
}

// Flush releases a Manual or paid link's outbox: the user pressed send and saw
// the cost. It returns what was spent.
func (m *Manager) Flush(name string) (frames int, cost int64, err error) {
	m.mu.Lock()
	l := m.links[name]
	if l == nil {
		m.mu.Unlock()
		return 0, 0, fmt.Errorf("transport: no link %q", name)
	}
	if !l.running {
		m.mu.Unlock()
		return 0, 0, fmt.Errorf("transport: %s is not running", name)
	}
	out := l.outbox
	l.outbox = nil
	ctx := m.ctx
	per := l.t.Caps().CostMicroUSD
	m.mu.Unlock()
	n, err := m.transmit(ctx, name, out)
	if err != nil {
		// What did not go stays queued, ahead of anything that arrived since.
		m.mu.Lock()
		l.outbox = append(append([][]byte(nil), out[n:]...), l.outbox...)
		m.mu.Unlock()
	}
	return n, per * int64(n), err
}

// Export drains a Manual link's outbox for a person to carry: the frames for a
// QR sequence or a file. Nothing is lost if the export is never used — the
// same messages still sync over the internet whenever that comes back.
func (m *Manager) Export(name string) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.links[name]
	if l == nil {
		return nil, fmt.Errorf("transport: no link %q", name)
	}
	out := l.outbox
	l.outbox = nil
	l.sent += len(out)
	return out, nil
}

// Receive feeds one frame that arrived on a link (or was imported from a QR
// code or file) into the stack: reassembly, freshness, dedupe, delivery, and
// relay.
func (m *Manager) Receive(name string, frame []byte) {
	whole, err := m.reasm.Add(frame)
	if err != nil || whole == nil {
		return
	}
	b, err := Unmarshal(whole)
	if err != nil {
		return
	}
	m.mu.Lock()
	if l := m.links[name]; l != nil {
		l.received++
	}
	today := Today(m.now())
	m.mu.Unlock()
	if !b.Fresh(today) || !m.seen.add(ID(b.Payload)) {
		return
	}
	m.mu.Lock()
	topic, ok := m.lookupLocked(b.Day, b.Tag)
	relay := m.settings.Relay && b.Hops > 0 && m.relayAllowedLocked()
	deliver := m.deliver
	m.mu.Unlock()
	if ok && deliver != nil {
		deliver(topic, b.Payload)
	}
	if relay {
		// A neighbour cannot buy a bundle more reach than its author could.
		b.Hops = min(b.Hops, DefaultHops) - 1
		m.dispatchRelay(b)
	}
}

// dispatchRelay forwards on live, free, local links only: a stranger's
// ciphertext never lands in this user's paid satellite outbox or their QR
// export.
func (m *Manager) dispatchRelay(b Bundle) {
	m.mu.Lock()
	type job struct {
		name   string
		frames [][]byte
	}
	var jobs []job
	for _, name := range m.sortedNames() {
		l := m.links[name]
		c := l.t.Caps()
		if !l.running || c.Mode != Live || c.Paid() || !c.Local || !m.eligibleLocked(c, len(b.Payload)) {
			continue
		}
		if frames, err := Frames(b, c.MTU); err == nil {
			jobs = append(jobs, job{name, frames})
		}
	}
	ctx := m.ctx
	m.mu.Unlock()
	for _, j := range jobs {
		go func() { _, _ = m.transmit(ctx, j.name, j.frames) }()
	}
}

func (m *Manager) relayAllowedLocked() bool {
	now := m.now()
	keep := m.relayed[:0]
	for _, t := range m.relayed {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	m.relayed = keep
	if len(m.relayed) >= relayPerMinute {
		return false
	}
	m.relayed = append(m.relayed, now)
	return true
}

func (m *Manager) lookupLocked(day uint16, tag [8]byte) (string, bool) {
	byTag, ok := m.tags[day]
	if !ok {
		byTag = make(map[[8]byte]string, len(m.topics))
		for t := range m.topics {
			byTag[TopicTag(t, day)] = t
		}
		m.tags[day] = byTag
	}
	t, ok := byTag[tag]
	return t, ok
}

// Links describes every registered link for the settings page.
func (m *Manager) Links() []LinkView {
	m.mu.Lock()
	names := m.sortedNames()
	type snap struct {
		l *link
		v LinkView
	}
	snaps := make([]snap, 0, len(names))
	for _, n := range names {
		l := m.links[n]
		c := l.t.Caps()
		snaps = append(snaps, snap{l, LinkView{
			Name: n, Label: l.t.Label(), Caps: c,
			Enabled: m.settings.Enabled[n], Running: l.running,
			Queued: len(l.outbox), QueuedCost: c.CostMicroUSD * int64(len(l.outbox)),
			Sent: l.sent, Received: l.received, Error: l.err,
		}})
	}
	m.mu.Unlock()
	out := make([]LinkView, len(snaps))
	for i, s := range snaps {
		s.v.Status = s.l.t.Probe() // may touch sysfs or a serial port; not under the lock
		out[i] = s.v
	}
	return out
}

// reconcile starts or stops one link to match the settings.
func (m *Manager) reconcile(name string) {
	m.mu.Lock()
	l := m.links[name]
	if l == nil {
		m.mu.Unlock()
		return
	}
	want := m.settings.Enabled[name] && (!m.settings.Offline || l.t.Caps().Local)
	running := l.running
	ctx := m.ctx
	m.mu.Unlock()
	switch {
	case want && !running:
		lctx, cancel := context.WithCancel(ctx)
		err := l.t.Start(lctx, func(f []byte) { m.Receive(name, f) })
		m.mu.Lock()
		if err != nil {
			cancel()
			l.err = err.Error()
		} else {
			l.running, l.cancel, l.err = true, cancel, ""
		}
		m.mu.Unlock()
	case !want && running:
		m.stop(name)
	}
}

func (m *Manager) stop(name string) {
	m.mu.Lock()
	l := m.links[name]
	if l == nil || !l.running {
		m.mu.Unlock()
		return
	}
	l.running = false
	cancel := l.cancel
	l.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = l.t.Stop()
}

func (m *Manager) sortedNames() []string {
	names := make([]string, 0, len(m.links))
	for n := range m.links {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (m *Manager) saveLocked() error {
	if m.path == "" {
		return nil
	}
	raw, _ := json.MarshalIndent(m.settings, "", "  ")
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// seenSet is a bounded FIFO set of payload IDs.
type seenSet struct {
	mu   sync.Mutex
	set  map[[16]byte]struct{}
	ring [][16]byte
	next int
}

func newSeenSet(n int) *seenSet {
	return &seenSet{set: make(map[[16]byte]struct{}, n), ring: make([][16]byte, n)}
}

// add records id and reports whether it was new.
func (s *seenSet) add(id [16]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[id]; ok {
		return false
	}
	old := s.ring[s.next]
	if old != ([16]byte{}) {
		delete(s.set, old)
	}
	s.ring[s.next] = id
	s.next = (s.next + 1) % len(s.ring)
	s.set[id] = struct{}{}
	return true
}
