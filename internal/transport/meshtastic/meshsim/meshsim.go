// Package meshsim is a fake Meshtastic mesh: nodes that speak the real client
// API (framed ToRadio/FromRadio protobufs, as on USB serial and TCP 4403) to
// whatever connects, and a simulated LoRa channel between them that loses,
// delays and floods packets the way Meshtastic's managed flooding does.
//
// Concord's tests run the production meshtastic.Link against it, and
// cmd/concord-meshsim exposes it on TCP for trying the app without radios.
package meshsim

import (
	"io"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/transport/meshtastic"
)

// Mesh is the simulated radio channel.
type Mesh struct {
	// Loss is the probability that one hop's transmission is not heard by a
	// given neighbour. Latency (+ up to Jitter) is one hop's airtime.
	Loss    float64
	Latency time.Duration
	Jitter  time.Duration

	mu     sync.Mutex
	rng    *rand.Rand
	nodes  map[uint32]*Node
	adj    map[uint32]map[uint32]bool // nil: everyone hears everyone
	closed bool
	timers map[*time.Timer]bool
	// Aired counts every over-the-air transmission, rebroadcasts included.
	Aired int
}

// New returns an empty mesh with a deterministic random source.
func New(seed int64) *Mesh {
	return &Mesh{rng: rand.New(rand.NewSource(seed)), nodes: map[uint32]*Node{}, timers: map[*time.Timer]bool{}}
}

// Node is one simulated Meshtastic device.
type Node struct {
	Num  uint32
	Name string
	// Licensed puts the node in amateur-radio mode (User.is_licensed).
	Licensed bool
	// Noise makes the node interleave debug-log text with its frames, as a
	// real board does on USB serial.
	Noise bool

	mesh    *Mesh
	mu      sync.Mutex
	clients map[*client]bool
	seen    map[uint32]bool
	seenQ   []uint32
	lns     []net.Listener
}

// AddNode adds a node with the given number.
func (m *Mesh) AddNode(num uint32, name string) *Node {
	n := &Node{Num: num, Name: name, mesh: m, clients: map[*client]bool{}, seen: map[uint32]bool{}}
	m.mu.Lock()
	m.nodes[num] = n
	m.mu.Unlock()
	return n
}

// Connect makes a and b hear each other. Once any pair is connected the mesh
// stops being fully connected and only explicit links carry packets.
func (m *Mesh) Connect(a, b *Node) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.adj == nil {
		m.adj = map[uint32]map[uint32]bool{}
	}
	for _, p := range [][2]uint32{{a.Num, b.Num}, {b.Num, a.Num}} {
		if m.adj[p[0]] == nil {
			m.adj[p[0]] = map[uint32]bool{}
		}
		m.adj[p[0]][p[1]] = true
	}
}

// Close stops every node, listener and in-flight packet.
func (m *Mesh) Close() {
	m.mu.Lock()
	m.closed = true
	for t := range m.timers {
		t.Stop()
	}
	m.timers = nil
	nodes := make([]*Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		nodes = append(nodes, n)
	}
	m.mu.Unlock()
	for _, n := range nodes {
		n.mu.Lock()
		lns := n.lns
		cs := make([]*client, 0, len(n.clients))
		for c := range n.clients {
			cs = append(cs, c)
		}
		n.mu.Unlock()
		for _, ln := range lns {
			ln.Close()
		}
		for _, c := range cs {
			c.conn.Close()
		}
	}
}

// air puts pkt on the channel from node from: every neighbour that hears it
// gets it after the hop latency.
func (m *Mesh) air(from uint32, pkt meshtastic.MeshPacket) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.Aired++
	for num, n := range m.nodes {
		if num == from || (m.adj != nil && !m.adj[from][num]) {
			continue
		}
		if m.Loss > 0 && m.rng.Float64() < m.Loss {
			continue
		}
		d := m.Latency
		if m.Jitter > 0 {
			d += time.Duration(m.rng.Int63n(int64(m.Jitter)))
		}
		p := pkt
		p.RxSNR = float32(m.rng.Intn(20)) - 10
		p.RxRSSI = -60 - int32(m.rng.Intn(60))
		var t *time.Timer
		t = time.AfterFunc(d, func() {
			m.mu.Lock()
			if m.timers != nil {
				delete(m.timers, t)
			}
			m.mu.Unlock()
			n.hear(p)
		})
		m.timers[t] = true
	}
}

// hear is a packet arriving over the air: deliver it to this node's clients,
// then rebroadcast while hops remain (managed flooding, deduplicated by id).
func (n *Node) hear(p meshtastic.MeshPacket) {
	n.mu.Lock()
	if n.seen[p.ID] || p.From == n.Num {
		n.mu.Unlock()
		return
	}
	n.remember(p.ID)
	cs := n.clientList()
	n.mu.Unlock()
	for _, c := range cs {
		cp := p
		c.send(&meshtastic.FromRadio{Packet: &cp})
	}
	if p.HopLimit > 0 {
		p.HopLimit--
		n.mesh.air(n.Num, p)
	}
}

func (n *Node) remember(id uint32) {
	n.seen[id] = true
	n.seenQ = append(n.seenQ, id)
	if len(n.seenQ) > 1024 {
		delete(n.seen, n.seenQ[0])
		n.seenQ = n.seenQ[1:]
	}
}

func (n *Node) clientList() []*client {
	cs := make([]*client, 0, len(n.clients))
	for c := range n.clients {
		cs = append(cs, c)
	}
	return cs
}

// Listen serves the client API on a TCP address ("127.0.0.1:0" for any port)
// and returns the bound address.
func (n *Node) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	n.mu.Lock()
	n.lns = append(n.lns, ln)
	n.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go n.Serve(c)
		}
	}()
	return ln.Addr().String(), nil
}

// Pipe returns the client end of an in-memory connection to the node.
func (n *Node) Pipe() io.ReadWriteCloser {
	a, b := net.Pipe()
	go n.Serve(b)
	return a
}

type client struct {
	conn  io.ReadWriteCloser
	wmu   sync.Mutex
	noise bool
}

const logLine = "INFO  | 12:00:00 42 [Router] Received routing from=0x0, id=0x94c3, portnum=5\r\n"

func (c *client) send(m *meshtastic.FromRadio) {
	f, err := meshtastic.EncodeFrame(m.Marshal())
	if err != nil {
		return
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.noise {
		// Log text, including a stray START1 byte, between real frames.
		_, _ = c.conn.Write(append([]byte(logLine), 0x94, 0x00, '\n'))
	}
	_, _ = c.conn.Write(f)
}

// Serve runs the client API on one connection until it closes.
func (n *Node) Serve(conn io.ReadWriteCloser) {
	c := &client{conn: conn, noise: n.Noise}
	n.mu.Lock()
	if n.mesh.isClosed() {
		n.mu.Unlock()
		conn.Close()
		return
	}
	n.clients[c] = true
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.clients, c)
		n.mu.Unlock()
		conn.Close()
	}()
	fr := meshtastic.NewFrameReader(conn)
	for {
		pb, err := fr.Next()
		if err != nil {
			return
		}
		m, err := meshtastic.UnmarshalToRadio(pb)
		if err != nil {
			continue
		}
		switch {
		case m.WantConfigID != 0:
			n.sendConfig(c, m.WantConfigID)
		case m.Packet != nil:
			n.transmit(c, *m.Packet)
		case m.Disconnect:
			return
		}
	}
}

func (m *Mesh) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (n *Node) sendConfig(c *client, id uint32) {
	c.send(&meshtastic.FromRadio{HasMyInfo: true, MyNodeNum: n.Num})
	n.mesh.mu.Lock()
	others := make([]*Node, 0, len(n.mesh.nodes))
	for _, o := range n.mesh.nodes {
		others = append(others, o)
	}
	n.mesh.mu.Unlock()
	for _, o := range others {
		c.send(&meshtastic.FromRadio{NodeInfo: &meshtastic.NodeInfo{Num: o.Num, LongName: o.Name, IsLicensed: o.Licensed}})
	}
	c.send(&meshtastic.FromRadio{HasConfigDone: true, ConfigCompleteID: id})
}

func (n *Node) transmit(c *client, p meshtastic.MeshPacket) {
	res := int32(0)
	if len(p.Payload) > meshtastic.DataPayloadLen {
		res = 1 // the real firmware refuses an oversized Data payload
	}
	c.send(&meshtastic.FromRadio{QueueStatus: &meshtastic.QueueStatus{Res: res, Free: 15, MaxLen: 16, PacketID: p.ID}})
	if res != 0 {
		return
	}
	p.From = n.Num
	if p.HopLimit > 7 {
		p.HopLimit = 7
	}
	p.HopStart = p.HopLimit
	n.mu.Lock()
	n.remember(p.ID)
	n.mu.Unlock()
	n.mesh.air(n.Num, p)
}
