//go:build linux && !android

// concord-blecheck exercises Concord's Bluetooth LE link on real hardware.
//
//	concord-blecheck -probe            # read-only: adapter, power, privacy
//	concord-blecheck -for 2m           # advertise, scan, exchange test frames
//
// Run the second form on two machines within a few metres of each other.
// Each sends a numbered test frame every -every and prints every frame it
// receives, checking its contents survived the air intact. The frames are
// random test bytes, never Concord messages.
//
// The link refuses adapters without LE privacy, as Concord does. On a test
// machine where Privacy cannot be enabled (it needs root), -unsafe-public-address
// runs anyway — and the adapter's fixed public address goes on air for as long
// as the check runs.
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ZahakJ/concord/internal/transport/ble"
)

const magic = 0xBE // not a bundle kind (0xC1/0xC2): the Manager would drop it

func main() {
	adapter := flag.String("adapter", "", "adapter name, e.g. hci0 (default: first)")
	probe := flag.Bool("probe", false, "only report availability; transmit nothing")
	unsafePublic := flag.Bool("unsafe-public-address", false, "run on an adapter without LE privacy (exposes its public address)")
	dur := flag.Duration("for", 60*time.Second, "how long to run")
	every := flag.Duration("every", 2*time.Second, "send a test frame this often (0: never)")
	size := flag.Int("size", ble.MaxFrame, "test frame size in bytes")
	rotate := flag.Duration("rotate", 0, "rotate the advertised nonce this often (0: the link's default, ~15m)")
	flag.Parse()

	radio := ble.NewBlueZ(ble.BlueZConfig{Adapter: *adapter, AllowPublicAddress: *unsafePublic})
	link := ble.New(radio)
	st := link.Probe()
	fmt.Printf("probe: %s %s\n", st.State, st.Detail)
	if *probe {
		return
	}
	if *size < 13 || *size > ble.MaxFrame {
		fmt.Fprintf(os.Stderr, "-size must be 13..%d\n", ble.MaxFrame)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *dur)
	defer cancel()

	var me [8]byte
	_, _ = rand.Read(me[:])
	var got, bad atomic.Int64
	deliver := func(f []byte) {
		if len(f) < 13 || f[0] != magic {
			fmt.Printf("recv %d bytes, not a blecheck frame: %x…\n", len(f), f[:min(len(f), 16)])
			return
		}
		from := hex.EncodeToString(f[1:9])
		seq := binary.BigEndian.Uint32(f[9:13])
		ok := true
		for i, c := range f[13:] {
			if c != byte(int(seq)+i) {
				ok = false
				break
			}
		}
		if ok {
			got.Add(1)
		} else {
			bad.Add(1)
		}
		fmt.Printf("%s recv from %s seq %d len %d intact=%v\n", time.Now().Format("15:04:05.000"), from, seq, len(f), ok)
	}
	if err := link.Start(ctx, deliver); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	ad, on := radio.AdvertisementData()
	fmt.Printf("up: me=%s advertising=%v service-data=%x service=%s\n", hex.EncodeToString(me[:]), on, ad, ble.ServiceUUID)

	var seq uint32
	var sendC <-chan time.Time
	if *every > 0 {
		t := time.NewTicker(*every)
		defer t.Stop()
		sendC = t.C
	}
	var rotC <-chan time.Time
	if *rotate > 0 {
		t := time.NewTicker(*rotate)
		defer t.Stop()
		rotC = t.C
	}
	status := time.NewTicker(5 * time.Second)
	defer status.Stop()
	var sent, sendErr int
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-sendC:
			seq++
			f := make([]byte, *size)
			f[0] = magic
			copy(f[1:9], me[:])
			binary.BigEndian.PutUint32(f[9:13], seq)
			for i := range f[13:] {
				f[13+i] = byte(int(seq) + i)
			}
			if err := link.Send(ctx, f); err != nil {
				sendErr++
				fmt.Println("send:", err)
			} else {
				sent++
			}
		case <-rotC:
			if err := link.Rotate(); err != nil {
				fmt.Println("rotate:", err)
			}
			ad, on := radio.AdvertisementData()
			fmt.Printf("rotated: advertising=%v service-data=%x\n", on, ad)
		case <-status.C:
			s := link.Stats()
			all, concord := radio.Counts()
			fmt.Printf("status: LE devices heard %d, Concord devices %d, dialed %d, subscribed-to-us %v, nonce %x\n",
				all, concord, s.Dialed, s.Subscribed, s.Nonce)
		}
	}
	if err := link.Stop(); err != nil {
		fmt.Println("stop:", err)
	}
	fmt.Printf("done: sent %d (errors %d), received %d intact, %d corrupted\n", sent, sendErr, got.Load(), bad.Load())
}
