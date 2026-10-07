package server

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mr-karan/logchef/internal/oauth"
)

// Handlers must not hand the fasthttp RequestCtx to code that takes a
// context.Context. Its Done channel is a fasthttp server field that Serve and
// Shutdown write without synchronization, while database driver goroutines
// started by a request keep reading it. Run under -race: DB-backed requests
// over a real socket (session auth, the OAuth token endpoint and /mcp), then
// a full Shutdown.
func TestServeAndShutdownAfterDBRequests(t *testing.T) {
	e := newOAuthEnv(t)
	token := e.mcpToken()
	baseURL, shutdown := e.serve()

	listTools, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/me", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/me: status %d, want 200", resp.StatusCode)
		}

		resp, err = http.PostForm(baseURL+oauth.TokenPath, map[string][]string{
			"grant_type": {"refresh_token"}, "client_id": {testWebClient}, "refresh_token": {"unknown"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST /oauth/token with an unknown refresh token: status %d, want 400", resp.StatusCode)
		}

		text, err := mcpPostOver(baseURL, token, listTools, map[string]string{"Mcp-Protocol-Version": protocolLegacy})
		if err != nil {
			t.Fatal(err)
		}
		var env mcpEnvelope
		if err := json.Unmarshal([]byte(text), &env); err != nil || env.Result == nil {
			t.Fatalf("tools/list: %s", text)
		}
	}
	shutdown()
}
