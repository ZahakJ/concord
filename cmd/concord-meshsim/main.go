// concord-meshsim runs a simulated Meshtastic mesh on localhost, each node
// serving the real client API on TCP, so Concord's LoRa link can be tried
// without radios:
//
//	concord-meshsim -nodes 3 -port 14403 -loss 0.1 -latency 1.5s -line
//	CONCORD_MESHTASTIC=tcp://127.0.0.1:14403 concord ...   # first instance
//	CONCORD_MESHTASTIC=tcp://127.0.0.1:14405 concord ...   # second instance
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZahakJ/concord/internal/transport/meshtastic/meshsim"
)

func main() {
	nodes := flag.Int("nodes", 2, "number of simulated nodes")
	port := flag.Int("port", 14403, "TCP port of the first node; the rest follow")
	host := flag.String("host", "127.0.0.1", "address to listen on")
	loss := flag.Float64("loss", 0.05, "per-hop probability a neighbour misses a packet")
	latency := flag.Duration("latency", 1500*time.Millisecond, "airtime per hop (LongFast, 200 B ≈ 1.6 s)")
	jitter := flag.Duration("jitter", 500*time.Millisecond, "random extra delay per hop")
	line := flag.Bool("line", false, "chain the nodes in a line (each hears only its neighbours) instead of a full mesh")
	flag.Parse()
	if *nodes < 1 || *nodes > 64 {
		log.Fatal("nodes must be 1..64")
	}
	m := meshsim.New(time.Now().UnixNano())
	m.Loss, m.Latency, m.Jitter = *loss, *latency, *jitter
	var all []*meshsim.Node
	for i := 0; i < *nodes; i++ {
		n := m.AddNode(uint32(0x5a000001+i), fmt.Sprintf("sim-%d", i+1))
		addr, err := n.Listen(fmt.Sprintf("%s:%d", *host, *port+2*i))
		if err != nil {
			log.Fatal(err)
		}
		if *line && i > 0 {
			m.Connect(all[i-1], n)
		}
		all = append(all, n)
		fmt.Printf("node !%08x  tcp://%s\n", n.Num, addr)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	m.Close()
}
