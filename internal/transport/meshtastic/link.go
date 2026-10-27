// Package meshtastic carries Concord bundles over a LoRa mesh through a
// Meshtastic node, speaking the node's own client API (the protobuf stream the
// phone apps use) over USB serial or TCP.
//
// Concord frames ride as Data{portnum: PRIVATE_APP} broadcasts on a channel.
// The node adds its channel AES on the air; Concord does not rely on it — what
// it hands the node is already MLS ciphertext plus the 12-byte bundle header.
package meshtastic

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/serialport"
)

const (
	// MTU stays well under DATA_PAYLOAD_LEN (233) so header growth in future
	// firmware (PKI, signatures) never truncates a frame.
	MTU = 200
	// serialBaud is the firmware's fixed client-API speed.
	serialBaud = 115200

	handshakeTimeout = 20 * time.Second
	heartbeatEvery   = 5 * time.Minute
	reconnectEvery   = 5 * time.Second
	queueWait        = 30 * time.Second
)

// Caps is the link as the settings page shows it. Bitrate is honest for the
// default LongFast preset: ~1.07 kbit/s PHY (SF11/BW250/CR4:5), a 200-byte
// frame is ~1.6 s on the air before preamble and rebroadcasts, and the
// firmware keeps channel utilisation polite (and EU868 enforces a 10% duty
// cycle). ~250 bit/s of useful throughput is what a chat line actually sees.
var Caps = transport.Caps{
	MTU:        MTU,
	Bitrate:    250,
	Latency:    3 * time.Second,
	Encryption: true, // ISM band (915/868/433 MHz): ciphertext is lawful
	Local:      true,
	Mode:       transport.Live,
	Range:      "1–10 km line of sight, more per mesh hop",
}

// Conn is an open stream to a node.
type Conn = io.ReadWriteCloser

// Dialer opens the stream to the node. The default dials Config.Device; tests
// and the simulator substitute their own.
type Dialer func(ctx context.Context) (Conn, error)

// ErrLicensed is why the link refuses a node in licensed (amateur) mode:
// FCC §97.113 forbids messages encoded to obscure their meaning, and Concord
// has nothing to send but ciphertext.
var ErrLicensed = errors.New("meshtastic: the node is in licensed (amateur radio) mode, where encrypted traffic is unlawful; turn off is_licensed to use it with Concord")

// Link is the Meshtastic transport.
type Link struct {
	cfg  Config
	dial Dialer

	mu      sync.Mutex
	conn    Conn
	myNum   uint32
	free    int // TX queue slots the node last reported; -1 unknown
	freeCh  chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
	deliver transport.Deliver
	lastErr string

	wmu sync.Mutex // serialises writes to conn
}

// New returns a link for cfg. dial may be nil to use cfg.Device.
func New(cfg Config, dial Dialer) *Link {
	return &Link{cfg: cfg.withDefaults(), dial: dial, free: -1}
}

func (l *Link) Name() string         { return "meshtastic" }
func (l *Link) Label() string        { return "LoRa mesh (Meshtastic)" }
func (l *Link) Caps() transport.Caps { return Caps }

// device resolves which node to use: the configured one, else the first USB
// device from a Meshtastic board vendor. It never opens anything.
func (l *Link) device() string {
	if l.cfg.Device != "" {
		return l.cfg.Device
	}
	if found := serialport.Find(usbVendors...); len(found) > 0 {
		return found[0]
	}
	return ""
}

// Probe looks for a node without contacting it: a configured TCP node is
// reported as configured, a serial node must exist in /dev.
func (l *Link) Probe() transport.Status {
	l.mu.Lock()
	connected, lastErr, num := l.conn != nil, l.lastErr, l.myNum
	l.mu.Unlock()
	if connected {
		return transport.Status{State: transport.Available, Detail: fmt.Sprintf("connected to node !%08x", num)}
	}
	if lastErr != "" {
		return transport.Status{State: transport.Failed, Detail: lastErr}
	}
	if l.dial != nil {
		return transport.Status{State: transport.Available, Detail: "simulated node"}
	}
	dev := l.device()
	if dev == "" {
		return transport.Status{State: transport.NoHardware, Detail: "no Meshtastic node on USB; set " + EnvDevice + " or meshtastic.json for a node on Wi-Fi"}
	}
	if addr, ok := tcpAddr(dev); ok {
		return transport.Status{State: transport.Available, Detail: "node at " + addr + " (not contacted until enabled)"}
	}
	if !serialport.Exists(dev) {
		return transport.Status{State: transport.NoHardware, Detail: dev + " is not present"}
	}
	return transport.Status{State: transport.Available, Detail: "node on " + dev}
}

func (l *Link) open(ctx context.Context) (Conn, error) {
	if l.dial != nil {
		return l.dial(ctx)
	}
	dev := l.device()
	if dev == "" {
		return nil, errors.New("meshtastic: no node found")
	}
	if addr, ok := tcpAddr(dev); ok {
		var d net.Dialer
		dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return d.DialContext(dctx, "tcp", addr)
	}
	p, err := serialport.Open(dev, serialBaud)
	if err != nil {
		return nil, err
	}
	// The Python client's wake-up: a run of START2 brings an ESP32 out of
	// light sleep and flushes a half-sent frame on the node's side.
	wake := make([]byte, 32)
	for i := range wake {
		wake[i] = start2
	}
	_, _ = p.Write(wake)
	return p, nil
}

// Start connects to the node and completes the want_config handshake, so a
// wrong port or a node in licensed mode is reported to the user at once.
// After that a supervisor reads packets and reconnects if the node drops.
func (l *Link) Start(ctx context.Context, deliver transport.Deliver) error {
	l.mu.Lock()
	if l.cancel != nil {
		l.mu.Unlock()
		return errors.New("meshtastic: already started")
	}
	l.mu.Unlock()
	conn, fr, err := l.connect(ctx)
	if err != nil {
		l.setErr(err)
		return err
	}
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.mu.Lock()
	l.deliver, l.cancel, l.done, l.lastErr = deliver, cancel, done, ""
	l.mu.Unlock()
	go l.run(lctx, conn, fr, done)
	return nil
}

// connect opens the stream and runs the handshake.
func (l *Link) connect(ctx context.Context) (Conn, *FrameReader, error) {
	conn, err := l.open(ctx)
	if err != nil {
		return nil, nil, err
	}
	fr := NewFrameReader(conn)
	num, err := l.handshake(ctx, conn, fr)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	l.mu.Lock()
	l.conn, l.myNum, l.free = conn, num, -1
	l.mu.Unlock()
	return conn, fr, nil
}

// handshake sends want_config_id and reads the node's config dump until
// config_complete_id echoes it, learning the node number and whether it is in
// licensed mode. Packets that arrive meanwhile are not ours yet and are
// dropped; the mesh is lossy and the bundle layer tolerates it.
func (l *Link) handshake(ctx context.Context, conn Conn, fr *FrameReader) (uint32, error) {
	id := randomID()
	if err := l.write(conn, &ToRadio{WantConfigID: id}); err != nil {
		return 0, err
	}
	type result struct {
		num uint32
		err error
	}
	res := make(chan result, 1)
	go func() {
		var num uint32
		var licensed bool
		nodes := map[uint32]bool{}
		for {
			pb, err := fr.Next()
			if err != nil {
				res <- result{err: fmt.Errorf("meshtastic: handshake: %w", err)}
				return
			}
			msg, err := UnmarshalFromRadio(pb)
			if err != nil {
				continue
			}
			switch {
			case msg.HasMyInfo:
				num = msg.MyNodeNum
			case msg.NodeInfo != nil:
				nodes[msg.NodeInfo.Num] = msg.NodeInfo.IsLicensed
			case msg.HasConfigDone && msg.ConfigCompleteID == id:
				licensed = nodes[num]
				if num == 0 {
					res <- result{err: errors.New("meshtastic: node sent no my_info")}
				} else if licensed {
					res <- result{err: ErrLicensed}
				} else {
					res <- result{num: num}
				}
				return
			}
		}
	}()
	t := time.NewTimer(handshakeTimeout)
	defer t.Stop()
	select {
	case r := <-res:
		return r.num, r.err
	case <-t.C:
		conn.Close() // unblocks the reader goroutine
		<-res
		return 0, errors.New("meshtastic: node did not answer the config request (wrong port, or not a Meshtastic node?)")
	case <-ctx.Done():
		conn.Close()
		<-res
		return 0, ctx.Err()
	}
}

// run reads from the node until the link stops, reconnecting after a drop.
func (l *Link) run(ctx context.Context, conn Conn, fr *FrameReader, done chan struct{}) {
	defer close(done)
	for {
		cctx, cancelConn := context.WithCancel(ctx)
		hbDone := make(chan struct{})
		go l.heartbeat(cctx, conn, hbDone)
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		err := l.read(fr)
		stop()
		cancelConn()
		conn.Close()
		<-hbDone
		l.mu.Lock()
		l.conn = nil
		l.free = -1
		l.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		l.setErr(fmt.Errorf("meshtastic: node connection lost: %v", err))
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectEvery):
			}
			var cerr error
			conn, fr, cerr = l.connect(ctx)
			if cerr == nil {
				l.setErr(nil)
				break
			}
			l.setErr(cerr)
			if errors.Is(cerr, ErrLicensed) {
				log.Printf("meshtastic: %v", cerr)
			}
		}
	}
}

func (l *Link) heartbeat(ctx context.Context, conn Conn, done chan struct{}) {
	defer close(done)
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if l.write(conn, &ToRadio{Heartbeat: true}) != nil {
				return
			}
		}
	}
}

// read handles FromRadio messages until the stream fails.
func (l *Link) read(fr *FrameReader) error {
	for {
		pb, err := fr.Next()
		if err != nil {
			return err
		}
		msg, err := UnmarshalFromRadio(pb)
		if err != nil {
			continue // a corrupted frame is noise
		}
		switch {
		case msg.QueueStatus != nil:
			l.mu.Lock()
			l.free = int(msg.QueueStatus.Free)
			if l.freeCh != nil && l.free > 0 {
				close(l.freeCh)
				l.freeCh = nil
			}
			l.mu.Unlock()
		case msg.Packet != nil:
			p := msg.Packet
			l.mu.Lock()
			mine, deliver := p.From == l.myNum, l.deliver
			l.mu.Unlock()
			if !p.Decoded || p.PortNum != l.cfg.Port || mine || len(p.Payload) == 0 || len(p.Payload) > DataPayloadLen {
				continue
			}
			if deliver != nil {
				deliver(p.Payload)
			}
		case msg.Rebooted:
			// The node lost our session; reconnecting re-runs the handshake.
			return errors.New("node rebooted")
		}
	}
}

// Stop disconnects from the node and waits for the reader to finish.
func (l *Link) Stop() error {
	l.mu.Lock()
	cancel, done, conn := l.cancel, l.done, l.conn
	l.cancel, l.done = nil, nil
	l.mu.Unlock()
	if cancel == nil {
		return nil
	}
	if conn != nil {
		_ = l.write(conn, &ToRadio{Disconnect: true})
	}
	cancel()
	<-done
	return nil
}

// Send broadcasts one frame on the configured channel. If the node reported a
// full transmit queue it waits for room rather than have the node drop it.
func (l *Link) Send(ctx context.Context, frame []byte) error {
	if len(frame) == 0 || len(frame) > MTU {
		return fmt.Errorf("meshtastic: frame of %d bytes (MTU %d)", len(frame), MTU)
	}
	deadline := time.NewTimer(queueWait)
	defer deadline.Stop()
	for {
		l.mu.Lock()
		conn, free := l.conn, l.free
		var wait chan struct{}
		if conn != nil && free == 0 {
			if l.freeCh == nil {
				l.freeCh = make(chan struct{})
			}
			wait = l.freeCh
		} else if free > 0 {
			l.free-- // optimistic until the node's next report
		}
		l.mu.Unlock()
		if conn == nil {
			return errors.New("meshtastic: not connected to a node")
		}
		if wait == nil {
			return l.write(conn, &ToRadio{Packet: &MeshPacket{
				To: BroadcastAddr, Channel: l.cfg.Channel, ID: randomID(),
				HopLimit: l.cfg.HopLimit, Decoded: true, PortNum: l.cfg.Port,
				Payload: frame,
			}})
		}
		select {
		case <-wait:
		case <-deadline.C:
			return errors.New("meshtastic: node transmit queue stayed full")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *Link) write(conn Conn, m *ToRadio) error {
	f, err := EncodeFrame(m.Marshal())
	if err != nil {
		return err
	}
	l.wmu.Lock()
	defer l.wmu.Unlock()
	// A node that stops reading must not wedge Send or Stop forever.
	if d, ok := conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = d.SetWriteDeadline(time.Now().Add(10 * time.Second))
	}
	_, err = conn.Write(f)
	return err
}

func (l *Link) setErr(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err == nil {
		l.lastErr = ""
	} else {
		l.lastErr = err.Error()
	}
}

func randomID() uint32 {
	var b [4]byte
	for {
		_, _ = rand.Read(b[:])
		if v := binary.BigEndian.Uint32(b[:]); v != 0 {
			return v
		}
	}
}
