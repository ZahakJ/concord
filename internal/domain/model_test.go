package domain

import "testing"

func TestNewIDUniqueAndSized(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if len(id) != 32 { // 16 bytes hex
			t.Fatalf("id length = %d, want 32", len(id))
		}
		if seen[id] {
			t.Fatal("duplicate ID generated")
		}
		seen[id] = true
	}
}

func TestNewGuildHasDefaultChannel(t *testing.T) {
	g := NewGuild("MyServer", []byte("group"), []byte("owner"))
	if g.Name != "MyServer" {
		t.Fatalf("name = %q", g.Name)
	}
	if len(g.Channels) != 1 || g.Channels[0].Name != "general" {
		t.Fatalf("expected a default #general channel, got %+v", g.Channels)
	}
	if g.Channels[0].GuildID != g.ID {
		t.Fatal("default channel not linked to guild")
	}
}

func TestNewMessageValidation(t *testing.T) {
	if _, err := NewMessage("", []byte("s"), "hi"); err == nil {
		t.Fatal("expected error for empty channel")
	}
	if _, err := NewMessage("chan", []byte("s"), ""); err == nil {
		t.Fatal("expected error for empty content")
	}
	m, err := NewMessage("chan", []byte("s"), "hi")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if m.ID == "" || m.Sent.IsZero() {
		t.Fatal("message missing ID or timestamp")
	}
}

func TestTopicDerivationDeterministicAndOpaque(t *testing.T) {
	groupA := []byte("group-a")
	groupB := []byte("group-b")

	// Deterministic: same inputs -> same topic.
	if TopicID(groupA, "chan1") != TopicID(groupA, "chan1") {
		t.Fatal("topic derivation is not deterministic")
	}
	// Distinct across channels and across guilds.
	if TopicID(groupA, "chan1") == TopicID(groupA, "chan2") {
		t.Fatal("different channels share a topic")
	}
	if TopicID(groupA, "chan1") == TopicID(groupB, "chan1") {
		t.Fatal("different guilds share a channel topic")
	}
	// Control topic distinct from channel topics.
	if ControlTopicID(groupA) == TopicID(groupA, "chan1") {
		t.Fatal("control topic collides with a channel topic")
	}
	// Opaque: the topic must not embed the channel id in the clear.
	if got := TopicID(groupA, "supersecret-channel"); contains(got, "supersecret") {
		t.Fatalf("topic leaks channel id: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
