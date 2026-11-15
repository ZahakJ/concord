package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// A yes to letting a recovery phrase or a link code replace the account on
// this device erases that account as surely as starting over does, so it is
// held to the same terms here, not only in the screen: the word typed
// (StartOverWord), and a ticket that is used once.
//
// The app's own confirm token (app.ConfirmToken) binds a yes to one incoming
// account over the keystore and data shown, but it is derived from them, the
// same every time and computable by anyone who can read the data directory.
// The screen therefore never sees it. It gets a random ticket standing for
// it, issued with each question; a ticket is spent by the attempt that uses
// it, and one that is unknown, spent or expired is passed on as no yes at
// all, so the app asks again (with a new ticket) and nothing is erased.

// replaceTTL bounds how long a replace question's ticket stays valid.
const replaceTTL = 10 * time.Minute

// replaceTicket is the outstanding replace question's ticket (one at a time:
// a new question replaces it).
type replaceTicket struct {
	ticket, token string
	issued        time.Time
}

// issueReplaceLocked stands a fresh ticket in for the app's token and returns
// it. Called with b.mu held.
func (b *Bridge) issueReplaceLocked(token string) string {
	if token == "" {
		return ""
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// No ticket: the screen's yes will be no yes, and it is asked again.
		b.replace = replaceTicket{}
		return ""
	}
	t := hex.EncodeToString(raw)
	b.replace = replaceTicket{ticket: t, token: token, issued: time.Now()}
	return t
}

func (b *Bridge) issueReplace(token string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.issueReplaceLocked(token)
}

// takeReplaceLocked turns the screen's yes into the app's token. No ticket:
// no yes (""). A ticket without the word typed is refused before anything is
// looked at, and is not spent. Otherwise the ticket is spent; an unknown,
// spent or expired one gives "", so the app asks again. Called with b.mu held.
func (b *Bridge) takeReplaceLocked(ticket, typed string) (string, error) {
	if ticket == "" {
		return "", nil
	}
	if !strings.EqualFold(strings.TrimSpace(typed), StartOverWord) {
		return "", &CodedError{Code: CodeConfirmWord, Msg: "Nothing was erased. To erase the account on this device, type " + StartOverWord + " to confirm."}
	}
	cur := b.replace
	if ticket != cur.ticket {
		return "", nil
	}
	b.replace = replaceTicket{}
	if time.Since(cur.issued) > replaceTTL {
		return "", nil
	}
	return cur.token, nil
}

func (b *Bridge) takeReplace(ticket, typed string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.takeReplaceLocked(ticket, typed)
}
