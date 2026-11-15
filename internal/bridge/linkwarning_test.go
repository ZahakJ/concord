package bridge

import (
	"strings"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// A link that could not join every handed-over guild must say which, and must
// not read as plain success. It used to return "" whenever the issuer handed
// every guild over, whatever happened to the joins.
func TestLinkWarningNamesGuildsThatWereNotJoined(t *testing.T) {
	if got := linkWarning(nil, nil); got != "" {
		t.Fatalf("everything arrived, want no warning, got %q", got)
	}

	got := linkWarning(nil, []appsvc.LinkJoinFailure{
		{GuildID: "a", Name: "Ours", Refused: true, Retrying: true, Err: "app: invite rejected: invite failed"},
		{GuildID: "b", Name: "Theirs", Retrying: true, Err: "net: dial: timeout"},
	})
	for _, want := range []string{"Linked — but", "couldn't join 2 guilds", "Ours, Theirs", "tries Ours, Theirs again", "earlier copy"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q lacks %q", got, want)
		}
	}

	got = linkWarning(nil, []appsvc.LinkJoinFailure{{GuildID: "b", Name: "Theirs", Err: "net: dial: timeout"}})
	if strings.Contains(got, "earlier copy") {
		t.Errorf("an unreachable committer is not a refusal: %q", got)
	}

	got = linkWarning([]string{"Far"}, []appsvc.LinkJoinFailure{{GuildID: "a", Name: "Ours", Refused: true}})
	if !strings.Contains(got, "Ours") || !strings.Contains(got, "Far") || !strings.Contains(got, "couldn't be handed over") {
		t.Errorf("both kinds must be named: %q", got)
	}

	// A guild that turned the device away for good is not promised a retry.
	got = linkWarning(nil, []appsvc.LinkJoinFailure{
		{GuildID: "c", Name: "Closed", Err: "app: invite rejected: banned"},
		{GuildID: "b", Name: "Theirs", Retrying: true, Err: "net: dial: timeout"},
	})
	if strings.Contains(got, "Closed again") || !strings.Contains(got, "tries Theirs again") || !strings.Contains(got, "Closed turned this device away") {
		t.Errorf("only retried guilds may be promised a retry: %q", got)
	}
	if strings.Contains(got, "earlier copy") {
		t.Errorf("a ban is not an earlier copy: %q", got)
	}
}
