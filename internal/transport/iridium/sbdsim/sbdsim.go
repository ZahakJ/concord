// Package sbdsim is a fake Iridium constellation with fake RockBLOCK 9603
// modems that answer the AT command set over an in-memory serial line:
// AT, ATE0/1, AT&K0, AT+CSQ, AT+SBDWB, AT+SBDIX(A), AT+SBDRB, AT+SBDD0/1/2,
// AT+SBDMTA, with Rock7's "RB<serial>" device-to-device routing, ring alerts,
// signal strength, failed sessions, and credit billing as Rock7 bills it.
//
// It models the modem's real hazard on purpose: a successful SBDIX does not
// clear the MO buffer, so a driver that forgets AT+SBDD0 re-sends (and pays
// for) the same message on its next mailbox check.
package sbdsim

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Constellation routes MO messages between modems in one delivery group.
type Constellation struct {
	mu     sync.Mutex
	rng    *rand.Rand
	modems map[string]*Modem
	// Signal is the AT+CSQ every modem reports (0–5); 0 fails every session.
	Signal int
	// FailRate is the probability a session drops (MO status 18).
	FailRate float64
	// SessionDelay is how long one SBDIX takes.
	SessionDelay time.Duration
}

// New returns a constellation with full signal.
func New(seed int64) *Constellation {
	return &Constellation{rng: rand.New(rand.NewSource(seed)), modems: map[string]*Modem{}, Signal: 5}
}

// Modem is one simulated RockBLOCK.
type Modem struct {
	Serial string // 7 digits
	c      *Constellation

	mu       sync.Mutex
	conn     io.ReadWriteCloser
	echo     bool
	ring     bool
	mo, mt   []byte
	mtQueue  [][]byte
	momsn    int
	mtmsn    int
	sessions int
	credits  int
	moSent   [][]byte
}

// AddModem adds a modem with the given serial (padded to 7 digits).
func (c *Constellation) AddModem(serial int) *Modem {
	m := &Modem{Serial: fmt.Sprintf("%07d", serial), c: c, echo: true}
	c.mu.Lock()
	c.modems[m.Serial] = m
	c.mu.Unlock()
	return m
}

// Pipe connects a driver to the modem, like plugging in its USB cable.
func (m *Modem) Pipe() io.ReadWriteCloser {
	a, b := net.Pipe()
	m.mu.Lock()
	m.conn = b
	m.echo = true // a fresh power-up echoes
	m.mu.Unlock()
	go m.serve(b)
	return a
}

// Credits billed to this modem's account so far.
func (m *Modem) Credits() int { m.mu.Lock(); defer m.mu.Unlock(); return m.credits }

// Sessions run so far.
func (m *Modem) Sessions() int { m.mu.Lock(); defer m.mu.Unlock(); return m.sessions }

// Sent is every MO that reached the gateway.
func (m *Modem) Sent() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.moSent...)
}

// Queue puts an MT message in this modem's gateway mailbox (as Rock7 would
// from its web API) and rings it.
func (m *Modem) Queue(msg []byte) {
	m.mu.Lock()
	m.mtQueue = append(m.mtQueue, append([]byte(nil), msg...))
	ring, conn := m.ring, m.conn
	m.mu.Unlock()
	if ring && conn != nil {
		_, _ = conn.Write([]byte("\r\nSBDRING\r\n"))
	}
}

func credits(n int) int { return (n + 49) / 50 }

func (m *Modem) reply(conn io.Writer, lines ...string) {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString("\r\n" + l + "\r\n")
	}
	_, _ = conn.Write(b.Bytes())
}

func (m *Modem) serve(conn io.ReadWriteCloser) {
	defer conn.Close()
	var line []byte
	buf := make([]byte, 256)
	pending := []byte(nil)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			c := pending[0]
			pending = pending[1:]
			if c == '\n' {
				continue
			}
			if c != '\r' {
				if len(line) < 128 {
					line = append(line, c)
				}
				continue
			}
			cmd := strings.ToUpper(strings.TrimSpace(string(line)))
			line = line[:0]
			m.mu.Lock()
			echo := m.echo
			m.mu.Unlock()
			if echo {
				_, _ = conn.Write([]byte(cmd + "\r"))
			}
			if want, ok := strings.CutPrefix(cmd, "AT+SBDWB="); ok {
				pending = m.sbdwb(conn, want, pending)
				continue
			}
			m.command(conn, cmd)
		}
	}
}

// sbdwb takes the binary upload that follows READY.
func (m *Modem) sbdwb(conn io.ReadWriteCloser, want string, pending []byte) []byte {
	n, err := strconv.Atoi(want)
	if err != nil || n < 1 || n > 340 {
		m.reply(conn, "3", "OK")
		return pending
	}
	m.reply(conn, "READY")
	buf := make([]byte, 256)
	for len(pending) < n+2 {
		k, err := conn.Read(buf)
		if err != nil {
			return nil
		}
		pending = append(pending, buf[:k]...)
	}
	msg, sum, rest := pending[:n], pending[n:n+2], pending[n+2:]
	var s uint16
	for _, c := range msg {
		s += uint16(c)
	}
	if sum[0] != byte(s>>8) || sum[1] != byte(s) {
		m.reply(conn, "2", "OK")
		return rest
	}
	m.mu.Lock()
	m.mo = append([]byte(nil), msg...)
	m.mu.Unlock()
	m.reply(conn, "0", "OK")
	return rest
}

func (m *Modem) command(conn io.ReadWriteCloser, cmd string) {
	switch cmd {
	case "":
		return
	case "AT", "AT&K0":
		m.reply(conn, "OK")
	case "ATE0", "ATE1":
		m.mu.Lock()
		m.echo = cmd == "ATE1"
		m.mu.Unlock()
		m.reply(conn, "OK")
	case "AT+SBDMTA=1", "AT+SBDMTA=0":
		m.mu.Lock()
		m.ring = cmd == "AT+SBDMTA=1"
		m.mu.Unlock()
		m.reply(conn, "OK")
	case "AT+CSQ":
		m.c.mu.Lock()
		s := m.c.Signal
		m.c.mu.Unlock()
		m.reply(conn, fmt.Sprintf("+CSQ:%d", s), "OK")
	case "AT+SBDD0", "AT+SBDD1", "AT+SBDD2":
		m.mu.Lock()
		if cmd != "AT+SBDD1" {
			m.mo = nil
		}
		if cmd != "AT+SBDD0" {
			m.mt = nil
		}
		m.mu.Unlock()
		m.reply(conn, "0", "OK")
	case "AT+SBDIX", "AT+SBDIXA":
		m.reply(conn, m.session(), "OK")
	case "AT+SBDRB":
		m.mu.Lock()
		mt := m.mt
		m.mu.Unlock()
		out := []byte{byte(len(mt) >> 8), byte(len(mt))}
		out = append(out, mt...)
		var s uint16
		for _, c := range mt {
			s += uint16(c)
		}
		out = append(out, byte(s>>8), byte(s))
		_, _ = conn.Write(out)
		m.reply(conn, "OK")
	default:
		m.reply(conn, "ERROR")
	}
}

// session is one SBDIX: send the MO buffer if any, then collect one MT.
func (m *Modem) session() string {
	c := m.c
	c.mu.Lock()
	delay, signal := c.SessionDelay, c.Signal
	fail := signal == 0 || (c.FailRate > 0 && c.rng.Float64() < c.FailRate)
	c.mu.Unlock()
	time.Sleep(delay)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions++
	if fail {
		status := 18
		if signal == 0 {
			status = 32
		}
		return fmt.Sprintf("+SBDIX: %d, %d, 2, %d, 0, %d", status, m.momsn, m.mtmsn, len(m.mtQueue))
	}
	if len(m.mo) > 0 {
		m.momsn++
		m.credits += credits(len(m.mo))
		m.moSent = append(m.moSent, append([]byte(nil), m.mo...))
		m.route(append([]byte(nil), m.mo...))
		// The real modem leaves the MO buffer full after success.
	}
	mtStatus, mtLen := 0, 0
	if len(m.mtQueue) > 0 {
		m.mt, m.mtQueue = m.mtQueue[0], m.mtQueue[1:]
		m.mtmsn++
		m.credits += credits(len(m.mt))
		mtStatus, mtLen = 1, len(m.mt)
	} else if len(m.mo) == 0 {
		m.credits++ // an empty mailbox check is billed one credit
	}
	return fmt.Sprintf("+SBDIX: 0, %d, %d, %d, %d, %d", m.momsn, mtStatus, m.mtmsn, mtLen, len(m.mtQueue))
}

// route forwards an "RB<serial>" message to that modem, as Rock7 does for
// devices in one delivery group (the prefix is removed on delivery).
// Called with m.mu held; delivery happens on another goroutine.
func (m *Modem) route(mo []byte) {
	if len(mo) < 9 || string(mo[:2]) != "RB" {
		return // to Rock7's HQ webhooks: no device receives it
	}
	m.c.mu.Lock()
	to := m.c.modems[string(mo[2:9])]
	m.c.mu.Unlock()
	if to == nil || to == m || len(mo)-9 > 270 {
		return
	}
	go to.Queue(mo[9:])
}
