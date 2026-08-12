package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// runREPL is a minimal line-oriented console for driving a Service from the
// terminal while the Wails GUI is unavailable. It is a development front end:
// the same Service API backs the eventual GUI, so anything demonstrated here is
// exercising the real stack.
func runREPL(ctx context.Context, svc *app.Service) error {
	// Live message feed.
	svc.OnMessage(func(m domain.Message) {
		fmt.Printf("\n  [%s] %s: %s\n> ", short(m.ChannelID), senderTag(m.Sender), m.Content)
	})
	svc.OnPeerConnected(func(p app.PeerPresence) {
		fmt.Printf("\n  * peer online %s\n> ", short(p.PeerID))
	})

	printHelp()
	fmt.Printf("PeerID: %s\nFingerprint: %s\n> ", svc.PeerID(), svc.Fingerprint())

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if cont := dispatch(svc, strings.TrimSpace(scanner.Text())); !cont {
			return nil
		}
		fmt.Print("> ")
	}
	return scanner.Err()
}

// dispatch runs one command; it returns false to exit the REPL.
func dispatch(svc *app.Service, line string) bool {
	if line == "" {
		return true
	}
	cmd, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)

	switch cmd {
	case "help", "?":
		printHelp()
	case "quit", "exit":
		return false
	case "whoami":
		fmt.Printf("PeerID: %s\nFingerprint: %s\n", svc.PeerID(), svc.Fingerprint())
	case "contacts":
		listContacts(svc)
	case "verify":
		if rest == "" {
			fmt.Println("usage: verify <peerID>  (see 'contacts')")
			break
		}
		if err := svc.VerifyContact(rest); err != nil {
			fmt.Println("error:", err)
			break
		}
		fmt.Println("marked verified:", rest)
	case "create":
		if rest == "" {
			fmt.Println("usage: create <guild name>")
			break
		}
		g, err := svc.CreateGuild(rest)
		if err != nil {
			fmt.Println("error:", err)
			break
		}
		fmt.Printf("created guild %q\n  guildID:   %s\n  channelID: %s (#%s)\n",
			g.Name, g.ID, g.Channels[0].ID, g.Channels[0].Name)
	case "guilds":
		listGuilds(svc)
	case "members":
		if rest == "" {
			fmt.Println("usage: members <guildID>")
			break
		}
		listMembers(svc, rest)
	case "kick":
		guildID, idxStr, ok := strings.Cut(rest, " ")
		if !ok {
			fmt.Println("usage: kick <guildID> <member#>  (see 'members')")
			break
		}
		kickMember(svc, guildID, strings.TrimSpace(idxStr))
	case "invite":
		if rest == "" {
			fmt.Println("usage: invite <guildID>")
			break
		}
		code, err := svc.InviteCode(rest)
		if err != nil {
			fmt.Println("error:", err)
			break
		}
		fmt.Printf("invite code (share out-of-band):\n%s\n", code)
	case "join":
		if rest == "" {
			fmt.Println("usage: join <invite code>")
			break
		}
		g, err := svc.JoinViaInvite(rest)
		if err != nil {
			fmt.Println("error:", err)
			break
		}
		fmt.Printf("joined guild %q (channel %s)\n", g.Name, g.Channels[0].ID)
	case "send":
		chID, text, ok := strings.Cut(rest, " ")
		if !ok || strings.TrimSpace(text) == "" {
			fmt.Println("usage: send <channelID> <message>")
			break
		}
		if _, err := svc.SendMessage(chID, strings.TrimSpace(text), "", ""); err != nil {
			fmt.Println("error:", err)
		}
	case "history":
		if rest == "" {
			fmt.Println("usage: history <channelID>")
			break
		}
		msgs, err := svc.Messages(rest, 50)
		if err != nil {
			fmt.Println("error:", err)
			break
		}
		for _, m := range msgs {
			fmt.Printf("  %s: %s\n", senderTag(m.Sender), m.Content)
		}
	default:
		fmt.Printf("unknown command %q (try: help)\n", cmd)
	}
	return true
}

func listGuilds(svc *app.Service) {
	guilds := svc.Guilds()
	if len(guilds) == 0 {
		fmt.Println("no guilds yet (create one, or join via invite)")
		return
	}
	for _, g := range guilds {
		n, _ := svc.MemberCount(g.ID)
		fmt.Printf("- %s  %q  (%d members)\n", g.ID, g.Name, n)
		for _, c := range g.Channels {
			fmt.Printf("    #%s  channelID=%s\n", c.Name, c.ID)
		}
	}
}

func listMembers(svc *app.Service, guildID string) {
	members, err := svc.GuildMembers(guildID)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	owner := svc.IsOwner(guildID)
	for i, cred := range members {
		fmt.Printf("  [%d] %s\n", i, identity.FingerprintOf(cred))
	}
	if owner {
		fmt.Println("  (you own this guild: use 'kick <guildID> <#>' to remove a member)")
	}
}

func kickMember(svc *app.Service, guildID, idxStr string) {
	members, err := svc.GuildMembers(guildID)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	var idx int
	if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil || idx < 0 || idx >= len(members) {
		fmt.Printf("invalid member number %q (see 'members %s')\n", idxStr, guildID)
		return
	}
	if err := svc.RemoveMember(guildID, members[idx]); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("removed member [%d] %s\n", idx, identity.FingerprintOf(members[idx]))
}

func listContacts(svc *app.Service) {
	contacts, err := svc.Contacts()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	if len(contacts) == 0 {
		fmt.Println("no contacts yet")
		return
	}
	for _, c := range contacts {
		mark := "unverified"
		if c.Verified {
			mark = "VERIFIED"
		}
		fmt.Printf("  %-10s %s  %s\n", mark, c.Fingerprint, c.PeerID)
	}
}

func printHelp() {
	fmt.Println(`commands:
  create <name>            create a guild (server)
  guilds                   list guilds and channel IDs
  members <guildID>        list guild members
  kick <guildID> <#>       remove a member (owner only)
  invite <guildID>         print an invite code to share
  join <code>              join a guild from an invite code
  send <channelID> <msg>   send a message
  history <channelID>      show stored history
  contacts                 list known peers + verification status
  verify <peerID>          mark a peer verified (after out-of-band check)
  whoami                   show this peer's identity
  help | quit`)
}

// senderTag renders a sender's account key as a short fingerprint prefix.
func senderTag(pub []byte) string {
	fpr := identity.FingerprintOf(pub)
	if len(fpr) > 9 {
		return fpr[:9]
	}
	return fpr
}

func short(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}
