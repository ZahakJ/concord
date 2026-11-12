package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/bridge"
)

// A locked core's refusal carries a machine-readable code beside the sentence,
// so the window branches on the code, not on the words.
func TestRPCErrorCarriesCode(t *testing.T) {
	t.Setenv("CONCORD_HOME", t.TempDir())
	s := NewWith(bridge.New(context.Background()), true)
	req := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(`{"method":"Guilds","args":[]}`))
	w := httptest.NewRecorder()
	s.HandleRPC(w, req)
	var resp struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != bridge.CodeLocked || resp.Error == "" {
		t.Fatalf("locked call: %+v", resp)
	}
}
