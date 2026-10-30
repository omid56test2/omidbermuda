package managed

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBermudaXrayConfigAndSubscription(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(CreateUserRequest{Name: "Omid Test", SubToken: "omid-test", InboundIDs: []string{"bermuda-vless-xhttp", "bermuda-vless-ws", "bermuda-trojan-ws"}})
	if err != nil {
		t.Fatal(err)
	}

	cfgBytes, err := store.BuildXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Listen   string `json:"listen"`
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []map[string]any `json:"clients"`
			} `json:"settings"`
			Stream struct {
				Network  string `json:"network"`
				Security string `json:"security"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 3 {
		t.Fatalf("expected 3 generated Xray listeners, got %d", len(cfg.Inbounds))
	}
	ports := map[int]bool{18443: false, 18444: false, 18445: false}
	for _, in := range cfg.Inbounds {
		if _, ok := ports[in.Port]; !ok {
			t.Fatalf("unexpected port %d", in.Port)
		}
		ports[in.Port] = true
		if in.Listen != "127.0.0.1" {
			t.Fatalf("inbound %d is not loopback-only: %s", in.Port, in.Listen)
		}
		if in.Stream.Security != "none" {
			t.Fatalf("expected TLS to terminate before local Xray listener, got %s", in.Stream.Security)
		}
		if len(in.Settings.Clients) != 1 {
			t.Fatalf("expected one client on port %d, got %d", in.Port, len(in.Settings.Clients))
		}
	}
	if !ports[18443] || !ports[18444] || !ports[18445] {
		t.Fatal("one or more Bermuda ports are missing")
	}
	if user.Configs[0].Secret != user.Configs[1].Secret {
		t.Fatal("VLESS XHTTP and WS should share the same UUID")
	}

	sub, err := store.BuildSubscription("omid-test", "https://edge.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Configs) != 3 || strings.Count(sub.Raw, "\n") != 2 {
		t.Fatalf("expected three subscription URIs, got %d", len(sub.Configs))
	}
	page, err := SubscriptionHTML(sub, "omid-test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "ZgotmplZ") || !strings.Contains(string(page), "href=\"vless://") || !strings.Contains(string(page), "href=\"trojan://") {
		t.Fatalf("custom client links were not rendered safely in the subscription page")
	}
	if !strings.Contains(string(page), "/assets/bermuda-qr.js") || !strings.Contains(string(page), "showQR('qr-") {
		t.Fatal("subscription page is missing its local per-config QR renderer")
	}
	for _, c := range sub.Configs {
		u, err := url.Parse(c.URI)
		if err != nil {
			t.Fatal(err)
		}
		if u.Host != "edge.example:443" {
			t.Fatalf("unexpected public endpoint: %s", u.Host)
		}
		if u.Query().Get("path") == "" {
			t.Fatalf("missing listener path: %s", c.URI)
		}
	}
}

func TestSubscriptionRejectsInactiveAndUnknownTokens(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BuildSubscription("missing", "https://edge.example"); err == nil {
		t.Fatal("expected unknown token rejection")
	}
	u, err := store.CreateUser(CreateUserRequest{Name: "Disabled", SubToken: "disabled", InboundIDs: []string{"bermuda-vless-ws"}})
	if err != nil {
		t.Fatal(err)
	}
	active := false
	if _, err := store.UpdateUser(u.UserID, UpdateUserRequest{Active: &active}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BuildSubscription("disabled", "https://edge.example"); err == nil {
		t.Fatal("expected inactive subscription rejection")
	}
}

func TestStage2InboundMigrationToBermudaListeners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-state.json")
	legacy := `{"users":{"user-1":{"user_id":"user-1","name":"Legacy","sub_token":"legacy-token","active":true,"inbound_ids":["builtin-vless-xhttp-packet-up","builtin-vless-ws","builtin-trojan-ws","builtin-vmess-ws"],"configs":[{"config_id":"cfg-1","inbound_id":"builtin-vless-xhttp-packet-up","protocol":"vless","secret":"11111111-1111-4111-8111-111111111111"},{"config_id":"cfg-2","inbound_id":"builtin-vless-ws","protocol":"vless","secret":"22222222-2222-4222-8222-222222222222"},{"config_id":"cfg-3","inbound_id":"builtin-trojan-ws","protocol":"trojan","secret":"trojan-secret"},{"config_id":"cfg-4","inbound_id":"builtin-vmess-ws","protocol":"vmess","secret":"33333333-3333-4333-8333-333333333333"}] }},"groups":{}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	users := store.ListUsers("")
	if len(users) != 1 {
		t.Fatalf("expected migrated user, got %d", len(users))
	}
	if len(users[0].Configs) != 3 {
		t.Fatalf("expected 3 supported migrated configs, got %d", len(users[0].Configs))
	}
	vless := map[string]string{}
	for _, c := range store.state.Users["user-1"].Configs {
		if c.Protocol == "vless" {
			vless[c.InboundID] = c.Secret
		}
	}
	if vless["bermuda-vless-xhttp"] == "" || vless["bermuda-vless-ws"] == "" || vless["bermuda-vless-xhttp"] != vless["bermuda-vless-ws"] {
		t.Fatalf("VLESS credentials were not unified during migration: %#v", vless)
	}
}
