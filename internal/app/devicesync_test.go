package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestMessageFromPhoneReachesTheDesktop is the reported symptom, reduced:
// "sending messages on phone don't show on desktop".
//
// Both devices are one account, both on the same rendezvous, both members of the
// same guild. This deliberately does NOT simulate carrier-grade NAT — the point
// is to separate the two candidate causes. If this passes, the linked-device
// message path itself is sound and the fault is reachability between the two
// machines; if it fails, the fault is in the app layer and no amount of network
// work would fix it.
func TestMessageFromPhoneReachesTheDesktop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	const body = "sent from the phone"
	if _, err := phone.SendMessage(textCh, body, ""); err != nil {
		t.Fatalf("phone SendMessage: %v", err)
	}

	waitUntil(t, 60*time.Second, func() bool {
		msgs, err := desk.Messages(textCh, 50)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if strings.Contains(m.Content, body) {
				return true
			}
		}
		return false
	}, "the desktop never received a message the phone sent")
}

// TestDesktopAndPhoneSeeEachOtherOnline is the other half of the report: the
// desktop appears in the phone's linked-device list but never says online.
// LinkedDevices is what that panel renders, so assert on it directly.
func TestDesktopAndPhoneSeeEachOtherOnline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	waitUntil(t, 60*time.Second, func() bool {
		for _, d := range phone.LinkedDevices() {
			if d.Online {
				return true
			}
		}
		return false
	}, "the phone never saw the desktop come online in its own device list")

	waitUntil(t, 60*time.Second, func() bool {
		for _, d := range desk.LinkedDevices() {
			if d.Online {
				return true
			}
		}
		return false
	}, "the desktop never saw the phone come online in its own device list")
}
