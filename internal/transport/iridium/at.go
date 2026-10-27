package iridium

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The 9602/9603 AT interface, as RockBLOCK exposes it on its FTDI USB port at
// 19200 8N1. Responses are CR LF delimited text, except the two binary
// transfers: AT+SBDWB uploads a message after "READY", and AT+SBDRB returns
// length ‖ message ‖ checksum. The modem may interject "SBDRING" (a ring
// alert: mail waiting) between anything.

const (
	rxCap       = 8 << 10 // bytes buffered from the modem before old ones go
	maxLine     = 512     // longer "lines" are line noise
	cmdTimeout  = 5 * time.Second
	csqTimeout  = 20 * time.Second // AT+CSQ waits for a fresh reading
	sbdTimeout  = 90 * time.Second // AT+SBDIX can take a minute
	readyTimout = 5 * time.Second
)

var (
	errATError  = errors.New("iridium: modem answered ERROR")
	errTimeout  = errors.New("iridium: modem did not answer")
	errClosed   = errors.New("iridium: modem port closed")
	errChecksum = errors.New("iridium: checksum mismatch")
)

// Checksum is the SBD checksum: the low 16 bits of the byte sum, big-endian.
func Checksum(b []byte) [2]byte {
	var s uint16
	for _, c := range b {
		s += uint16(c)
	}
	return [2]byte{byte(s >> 8), byte(s)}
}

// modem drives one 9602/9603. A reader goroutine fills a bounded buffer; every
// command runs on the caller's goroutine, one at a time (callers hold the
// link's session lock).
type modem struct {
	port io.ReadWriteCloser

	mu     sync.Mutex
	buf    []byte
	closed bool
	// idle is true between conversations: anything the modem says then is
	// unsolicited, and the only unsolicited thing worth hearing is SBDRING.
	// During a conversation the command reader handles it, so MT bytes in
	// an SBDRB transfer are never mistaken for an alert.
	idle   bool
	notify chan struct{} // poked when bytes arrive or the port closes
	ring   chan struct{} // poked on SBDRING
	done   chan struct{}
}

func newModem(port io.ReadWriteCloser) *modem {
	m := &modem{port: port, notify: make(chan struct{}, 1), ring: make(chan struct{}, 1), done: make(chan struct{})}
	go m.reader()
	return m
}

func (m *modem) reader() {
	defer close(m.done)
	b := make([]byte, 512)
	for {
		n, err := m.port.Read(b)
		m.mu.Lock()
		if n > 0 {
			m.buf = append(m.buf, b[:n]...)
			if over := len(m.buf) - rxCap; over > 0 {
				m.buf = append(m.buf[:0], m.buf[over:]...)
			}
			if m.idle {
				m.scanIdleLocked()
			}
		}
		if err != nil {
			m.closed = true
		}
		m.mu.Unlock()
		m.poke()
		if err != nil {
			return
		}
	}
}

func (m *modem) poke() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

// scanIdleLocked consumes complete unsolicited lines, keeping ring alerts.
func (m *modem) scanIdleLocked() {
	for {
		if _, ok := m.lineLocked(); !ok {
			return
		}
	}
}

// busy marks the start (true) or end (false) of a conversation.
func (m *modem) busy(on bool) {
	m.mu.Lock()
	m.idle = !on
	if m.idle {
		m.scanIdleLocked()
	}
	m.mu.Unlock()
}

// close shuts the port and waits for the reader.
func (m *modem) close() {
	m.port.Close()
	<-m.done
}

// wait blocks until more bytes may be available or the deadline passes.
func (m *modem) wait(deadline time.Time) error {
	d := time.Until(deadline)
	if d <= 0 {
		return errTimeout
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-m.notify:
		return nil
	case <-t.C:
		return errTimeout
	}
}

// line returns the next non-empty response line, consuming SBDRING alerts on
// the way. It must hold m.mu.
func (m *modem) lineLocked() (string, bool) {
	for {
		i := bytes.IndexAny(m.buf, "\r\n")
		if i < 0 {
			if len(m.buf) > maxLine {
				m.buf = m.buf[:0] // a run of garbage with no line end
			}
			return "", false
		}
		raw := m.buf[:i]
		m.buf = m.buf[i+1:]
		if len(raw) > maxLine {
			continue
		}
		s := strings.TrimSpace(string(raw))
		if s == "" {
			continue
		}
		if s == "SBDRING" {
			select {
			case m.ring <- struct{}{}:
			default:
			}
			continue
		}
		return s, true
	}
}

func (m *modem) readLine(deadline time.Time) (string, error) {
	for {
		m.mu.Lock()
		s, ok := m.lineLocked()
		closed := m.closed
		m.mu.Unlock()
		if ok {
			return s, nil
		}
		if closed {
			return "", errClosed
		}
		if err := m.wait(deadline); err != nil {
			return "", err
		}
	}
}

func (m *modem) readN(n int, deadline time.Time) ([]byte, error) {
	for {
		m.mu.Lock()
		if len(m.buf) >= n {
			out := append([]byte(nil), m.buf[:n]...)
			m.buf = m.buf[n:]
			m.mu.Unlock()
			return out, nil
		}
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return nil, errClosed
		}
		if err := m.wait(deadline); err != nil {
			return nil, err
		}
	}
}

// discard drops whatever the modem said that nobody asked for, keeping any
// ring alert in it.
func (m *modem) discard() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for {
		if _, ok := m.lineLocked(); !ok {
			break
		}
	}
	m.buf = m.buf[:0]
}

func (m *modem) write(b []byte) error {
	_, err := m.port.Write(b)
	return err
}

// cmd sends one AT command and returns the information lines before OK.
func (m *modem) cmd(c string, timeout time.Duration) ([]string, error) {
	m.discard()
	if err := m.write([]byte(c + "\r")); err != nil {
		return nil, err
	}
	return m.collect(c, time.Now().Add(timeout))
}

// collect reads lines until OK or ERROR, dropping the command's echo.
func (m *modem) collect(c string, deadline time.Time) ([]string, error) {
	var info []string
	for {
		s, err := m.readLine(deadline)
		if err != nil {
			return info, err
		}
		switch {
		case s == "OK":
			return info, nil
		case s == "ERROR":
			return info, errATError
		case strings.EqualFold(s, c):
			// echo (ATE1, before init turns it off)
		default:
			if len(info) < 16 {
				info = append(info, s)
			}
		}
	}
}

// init brings the modem to a known state: echo off, no hardware flow control
// (RockBLOCK's 3-wire serial), ring alerts on, and an empty MO buffer — so a
// later mailbox check can never transmit (and bill) a stale message.
func (m *modem) init(ringAlerts bool) error {
	var err error
	for i := 0; i < 3; i++ { // the first AT after power-up is often lost
		if _, err = m.cmd("AT", 2*time.Second); err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("iridium: no answer to AT (is this a RockBLOCK at 19200 baud?): %w", err)
	}
	steps := []string{"ATE0", "AT&K0", "AT+SBDD0"}
	if ringAlerts {
		steps = append(steps, "AT+SBDMTA=1")
	}
	for _, c := range steps {
		if _, err := m.cmd(c, cmdTimeout); err != nil {
			return fmt.Errorf("iridium: %s: %w", c, err)
		}
	}
	return nil
}

// signal is AT+CSQ: 0 (none) to 5 bars.
func (m *modem) signal() (int, error) {
	info, err := m.cmd("AT+CSQ", csqTimeout)
	if err != nil {
		return 0, err
	}
	for _, s := range info {
		if v, ok := strings.CutPrefix(s, "+CSQ:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > 5 {
				return 0, fmt.Errorf("iridium: bad CSQ %q", s)
			}
			return n, nil
		}
	}
	return 0, fmt.Errorf("iridium: no +CSQ in %q", info)
}

// writeMO is AT+SBDWB: load msg into the MO buffer (nothing is sent yet).
func (m *modem) writeMO(msg []byte) error {
	if len(msg) == 0 || len(msg) > MOLimit {
		return fmt.Errorf("iridium: MO of %d bytes (limit %d)", len(msg), MOLimit)
	}
	c := fmt.Sprintf("AT+SBDWB=%d", len(msg))
	m.discard()
	if err := m.write([]byte(c + "\r")); err != nil {
		return err
	}
	deadline := time.Now().Add(readyTimout)
	for {
		s, err := m.readLine(deadline)
		if err != nil {
			return fmt.Errorf("iridium: waiting for READY: %w", err)
		}
		if s == "READY" {
			break
		}
		if s == "ERROR" {
			return errATError
		}
		if s == "3" { // wrong size, refused before READY
			return errors.New("iridium: modem refused the message size")
		}
	}
	sum := Checksum(msg)
	if err := m.write(append(append([]byte(nil), msg...), sum[0], sum[1])); err != nil {
		return err
	}
	info, err := m.collect(c, time.Now().Add(cmdTimeout))
	if err != nil {
		return err
	}
	for _, s := range info {
		switch s {
		case "0":
			return nil
		case "1":
			return errors.New("iridium: SBDWB timed out")
		case "2":
			return errChecksum
		case "3":
			return errors.New("iridium: modem refused the message size")
		}
	}
	return fmt.Errorf("iridium: unexpected SBDWB answer %q", info)
}

// clearMO is AT+SBDD0.
func (m *modem) clearMO() error {
	info, err := m.cmd("AT+SBDD0", cmdTimeout)
	if err != nil {
		return err
	}
	for _, s := range info {
		if s == "1" {
			return errors.New("iridium: SBDD0 failed")
		}
	}
	return nil
}

// session runs AT+SBDIX (or AT+SBDIXA when answering a ring alert).
func (m *modem) session(answer bool) (SBDIX, error) {
	c := "AT+SBDIX"
	if answer {
		c = "AT+SBDIXA"
	}
	info, err := m.cmd(c, sbdTimeout)
	if err != nil {
		return SBDIX{}, err
	}
	for _, s := range info {
		if strings.HasPrefix(s, "+SBDIX:") {
			return ParseSBDIX(s)
		}
	}
	return SBDIX{}, fmt.Errorf("iridium: no +SBDIX in %q", info)
}

// readMT is AT+SBDRB: length ‖ message ‖ checksum, then OK.
func (m *modem) readMT() ([]byte, error) {
	m.discard()
	if err := m.write([]byte("AT+SBDRB\r")); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(cmdTimeout)
	// Tolerate an echo if init's ATE0 was lost.
	if hdr, err := m.peek(9, deadline); err == nil && string(hdr) == "AT+SBDRB\r" {
		_, _ = m.readN(9, deadline)
		if nl, err := m.peek(1, deadline); err == nil && nl[0] == '\n' {
			_, _ = m.readN(1, deadline)
		}
	}
	// A ring alert can land just before the binary reply. A real length is
	// at most 0x010E, so a leading CR, LF or 'S' can only be that text.
	for {
		b, err := m.peek(1, deadline)
		if err != nil {
			return nil, err
		}
		if b[0] == '\r' || b[0] == '\n' {
			_, _ = m.readN(1, deadline)
			continue
		}
		if b[0] == 'S' {
			if w, err := m.peek(7, deadline); err == nil && string(w) == "SBDRING" {
				_, _ = m.readN(7, deadline)
				select {
				case m.ring <- struct{}{}:
				default:
				}
				continue
			}
		}
		break
	}
	lb, err := m.readN(2, deadline)
	if err != nil {
		return nil, err
	}
	n := int(lb[0])<<8 | int(lb[1])
	if n > MTLimit {
		m.discard()
		return nil, fmt.Errorf("iridium: MT length %d exceeds %d", n, MTLimit)
	}
	body, err := m.readN(n+2, deadline)
	if err != nil {
		return nil, err
	}
	msg, sum := body[:n], body[n:]
	if want := Checksum(msg); sum[0] != want[0] || sum[1] != want[1] {
		m.discard()
		return nil, errChecksum
	}
	if _, err := m.collect("AT+SBDRB", deadline); err != nil {
		return nil, err
	}
	return msg, nil
}

// peek returns the first n buffered bytes without consuming them.
func (m *modem) peek(n int, deadline time.Time) ([]byte, error) {
	for {
		m.mu.Lock()
		if len(m.buf) >= n {
			out := append([]byte(nil), m.buf[:n]...)
			m.mu.Unlock()
			return out, nil
		}
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return nil, errClosed
		}
		if err := m.wait(deadline); err != nil {
			return nil, err
		}
	}
}
