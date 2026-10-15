package bridge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// BenchmarkGuilds is the sidebar refresh the frontend runs on every presence
// change and guild update: a dozen guilds of a dozen channels, one with a
// forum of forty posts, a few custom emoji each, thirty DMs, and a history big
// enough that a full scan would show.
func BenchmarkGuilds(b *testing.B) {
	dir := b.TempDir()
	svc, err := appsvc.Start(context.Background(), appsvc.Config{DataDir: dir, Passphrase: "bench-pass", DisableMDNS: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = svc.Close() })

	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89}, 12<<10))
	var channels []string
	for g := 0; g < 12; g++ {
		guild, err := svc.CreateGuild(fmt.Sprintf("guild %d", g))
		if err != nil {
			b.Fatal(err)
		}
		for _, c := range guild.Channels {
			channels = append(channels, c.ID)
		}
		for c := 0; c < 11; c++ {
			ch, err := svc.CreateChannel(guild.ID, fmt.Sprintf("chan-%d", c), "text", "")
			if err != nil {
				b.Fatal(err)
			}
			channels = append(channels, ch.ID)
		}
		for e := 0; e < 4; e++ {
			if err := svc.AddCustomEmoji(guild.ID, fmt.Sprintf("e%d_%d", g, e), png); err != nil {
				b.Fatal(err)
			}
		}
		if g == 0 {
			forum, err := svc.CreateChannel(guild.ID, "forum", "forum", "")
			if err != nil {
				b.Fatal(err)
			}
			for p := 0; p < 40; p++ {
				post, err := svc.CreateThread(guild.ID, forum.ID, fmt.Sprintf("post %d", p), "body", nil)
				if err != nil {
					b.Fatal(err)
				}
				channels = append(channels, post.ID)
			}
		}
	}
	for d := 0; d < 30; d++ {
		dm, err := svc.StartDM(fmt.Sprintf("%040x", d+1))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := svc.SendMessage(dm.Channels[0].ID, "see you there", "", ""); err != nil {
			b.Fatal(err)
		}
	}

	// History, written behind the service's back: the sidebar reads only the
	// metadata columns, so the bodies need not decrypt.
	db, err := sql.Open("sqlite", filepath.Join(dir, "concord.db"))
	if err != nil {
		b.Fatal(err)
	}
	tx, _ := db.Begin()
	now := time.Now().UnixNano()
	for i := 0; i < 60000; i++ {
		ch := channels[i%len(channels)]
		if _, err := tx.Exec(`INSERT INTO messages (id, channel_id, sender, content_enc, nonce, sent, updated) VALUES (?,?,?,?,?,?,?)`,
			fmt.Sprintf("bench-%06d", i), ch, []byte{1}, []byte("x"), []byte("n"), now-int64(i)*1e9, now-int64(i)*1e9); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	_ = db.Close()

	br := &Bridge{ctx: context.Background(), svc: svc}
	views, err := br.Guilds()
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("%d guild views", len(views))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := br.Guilds(); err != nil {
			b.Fatal(err)
		}
	}
}
