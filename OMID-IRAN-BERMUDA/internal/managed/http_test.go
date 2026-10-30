package managed

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedAPIAuthAndUserFlow(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&HTTPHandler{Store: store, AdminToken: "01234567890123456789012345678901", PublicBaseURL: "https://edge.example"}).Register(mux)
	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest("GET", "/api/inbounds", nil))
	if unauth.Code != 401 {
		t.Fatalf("expected 401, got %d", unauth.Code)
	}
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var b bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&b).Encode(body)
		}
		r := httptest.NewRequest(method, path, &b)
		r.Header.Set("Authorization", "Bearer 01234567890123456789012345678901")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	ins := call("GET", "/api/inbounds", nil)
	if ins.Code != 200 {
		t.Fatalf("inbounds status: %d %s", ins.Code, ins.Body.String())
	}
	var inboundReply struct {
		Inbounds []Inbound `json:"inbounds"`
	}
	if err := json.Unmarshal(ins.Body.Bytes(), &inboundReply); err != nil {
		t.Fatal(err)
	}
	if len(inboundReply.Inbounds) != 3 {
		t.Fatalf("expected 3 Bermuda listeners; got %d", len(inboundReply.Inbounds))
	}
	ids := []string{}
	for _, in := range inboundReply.Inbounds {
		ids = append(ids, in.InboundID)
	}
	created := call("POST", "/api/users", map[string]any{"name": "Omid", "inbound_ids": ids, "expires_days": 0, "limit_value": 0, "limit_unit": "GB", "ip_limit": 0, "connection_limit": 0, "speed_limit_value": 0, "speed_limit_unit": "MBIT"})
	if created.Code != 201 {
		t.Fatalf("create status: %d %s", created.Code, created.Body.String())
	}
	var c struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.UserID == "" {
		t.Fatal("missing user id")
	}
	listed := call("GET", "/api/users", nil)
	if listed.Code != 200 {
		t.Fatalf("list status: %d", listed.Code)
	}
	var list struct {
		Users []UserView `json:"users"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Users) != 1 || list.Users[0].ConfigCount != 3 {
		t.Fatalf("unexpected users list: %+v", list)
	}
	// Browser receives the visual subscription, clients receive Base64 URI content.
	user := store.ListUsers("https://edge.example")[0]
	browserReq := httptest.NewRequest("GET", "/sub/"+user.SubToken+"?view=1", nil)
	browserReq.Header.Set("Accept", "text/html")
	browser := httptest.NewRecorder()
	mux.ServeHTTP(browser, browserReq)
	if browser.Code != 200 || !strings.Contains(browser.Body.String(), "VLESS") || !strings.Contains(browser.Body.String(), "trojan://") {
		t.Fatalf("unexpected visual subscription response: %d", browser.Code)
	}
	clientReq := httptest.NewRequest("GET", "/sub/"+user.SubToken, nil)
	client := httptest.NewRecorder()
	mux.ServeHTTP(client, clientReq)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(client.Body.String()))
	if err != nil || !strings.Contains(string(decoded), "vless://") || !strings.Contains(string(decoded), "trojan://") {
		t.Fatalf("invalid Base64 client subscription: %v", err)
	}
	if (!strings.Contains(string(decoded), "/connect/") && !strings.Contains(string(decoded), "%2Fconnect%2F")) || strings.Contains(string(decoded), "/api/v1/") {
		t.Fatalf("Stage 5 subscription must use per-user Gateway routes, got %q", string(decoded))
	}
	base64Req := httptest.NewRequest("GET", "/sub/base64/"+user.SubToken, nil)
	base64Reply := httptest.NewRecorder()
	mux.ServeHTTP(base64Reply, base64Req)
	if base64Reply.Code != 200 || strings.TrimSpace(base64Reply.Body.String()) != strings.TrimSpace(client.Body.String()) {
		t.Fatalf("explicit Base64 endpoint does not match default client endpoint")
	}

	blocked := call("POST", "/api/inbounds", map[string]any{"name": "custom"})
	if blocked.Code != 403 {
		t.Fatalf("inbound mutation should be forbidden, got %d", blocked.Code)
	}
}
