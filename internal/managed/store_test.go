package managed

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedInboundsAndUserLifecycle(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ins := s.Inbounds()
	if len(ins) != 3 {
		t.Fatalf("expected 3 Bermuda listeners, got %d", len(ins))
	}
	want := map[string]int{"bermuda-vless-xhttp": 18443, "bermuda-vless-ws": 18444, "bermuda-trojan-ws": 18445}
	for _, in := range ins {
		if in.Listen != "127.0.0.1" || in.Port != want[in.InboundID] || !strings.HasPrefix(in.Path, "/api/v1/") {
			t.Fatalf("unexpected Bermuda listener definition: %+v", in)
		}
	}
	ids := make([]string, 0, len(ins))
	for _, in := range ins {
		ids = append(ids, in.InboundID)
	}
	u, err := s.CreateUser(CreateUserRequest{Name: "Test user", InboundIDs: ids, LimitValue: 0, LimitUnit: "GB", ExpiresDays: 0, IPLimit: 0, SpeedLimitValue: 0, SpeedLimitUnit: "MBIT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Configs) != 3 {
		t.Fatalf("expected 3 generated configs, got %d", len(u.Configs))
	}
	secretByProtocol := map[string]string{}
	for _, c := range u.Configs {
		if prior := secretByProtocol[c.Protocol]; prior != "" && prior != c.Secret {
			t.Fatalf("Bermuda-style credentials should be shared by protocol; mismatch for %s", c.Protocol)
		}
		secretByProtocol[c.Protocol] = c.Secret
	}
	if u.LimitBytes != 0 || u.SpeedLimitBytes != 0 || u.IPLimit != 0 || u.ConnectionLimit != 0 {
		t.Fatal("zero limits must mean unlimited")
	}
	listed := s.ListUsers("")
	if len(listed) != 1 {
		t.Fatalf("expected one user, got %d", len(listed))
	}
	for _, c := range listed[0].Configs {
		if c.Secret != "" {
			t.Fatal("secret leaked in users list")
		}
	}
	// Verify durable persistence and that credentials remain available internally.
	s2, err := NewStore(filepath.Join(filepath.Dir(s.path), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s2.ListUsers("")[0].Configs); got != 3 {
		t.Fatalf("persisted configs: got %d", got)
	}
	if err := s2.DeleteUser(u.UserID); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsInvalidInboundAndToken(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateUser(CreateUserRequest{Name: "Bad", InboundIDs: []string{"not-an-inbound"}})
	if err == nil {
		t.Fatal("expected invalid inbound rejection")
	}
	_, err = s.CreateUser(CreateUserRequest{Name: "Bad", SubToken: "bad token", InboundIDs: []string{"bermuda-vless-ws"}})
	if err == nil {
		t.Fatal("expected invalid token rejection")
	}
}

func TestGroupAndPartialUpdateSemantics(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGroup("Family", "private group")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(CreateUserRequest{Name: "Alice", SubID: g.SubID, InboundIDs: []string{"bermuda-vless-ws"}, ExpiresDays: 0, LimitValue: 12, LimitUnit: "GB", IPLimit: 2, ConnectionLimit: 3, SpeedLimitValue: 10, SpeedLimitUnit: "MBIT"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Alice Updated"
	updated, err := s.UpdateUser(u.UserID, UpdateUserRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LimitBytes != u.LimitBytes || updated.IPLimit != 2 || updated.ConnectionLimit != 3 || updated.SpeedLimitBytes != u.SpeedLimitBytes {
		t.Fatal("partial update changed omitted policy fields")
	}
	if err := s.DeleteGroup(g.SubID); err == nil {
		t.Fatal("group assigned to a user must not be deleted")
	}
	if err := s.DeleteUser(u.UserID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(g.SubID); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayRoutesAndBackupRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(CreateUserRequest{Name: "Backup User", InboundIDs: []string{"bermuda-vless-ws", "bermuda-trojan-ws"}, IPLimit: 2, ConnectionLimit: 3, SpeedLimitValue: 5, SpeedLimitUnit: "MBIT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.GatewayRoutes()) != 2 {
		t.Fatalf("expected 2 managed gateway routes, got %d", len(s.GatewayRoutes()))
	}
	for _, route := range s.GatewayRoutes() {
		if route.UserID != u.UserID || route.RouteToken == "" || !strings.HasPrefix(route.UpstreamPath, "/api/v1/") {
			t.Fatalf("invalid gateway route: %+v", route)
		}
	}
	for _, item := range s.ListUsers("") {
		for _, cfg := range item.Configs {
			if cfg.Secret != "" || cfg.RouteToken != "" {
				t.Fatal("private credentials leaked through user list")
			}
		}
	}
	backup, err := s.ExportBackup()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(u.UserID); err != nil {
		t.Fatal(err)
	}
	if len(s.GatewayRoutes()) != 0 {
		t.Fatal("deleted user must not have gateway routes")
	}
	if err := s.RestoreBackup(backup); err != nil {
		t.Fatal(err)
	}
	if len(s.ListUsers("")) != 1 || len(s.GatewayRoutes()) != 2 {
		t.Fatal("backup restore did not recover the user and runtime routes")
	}
	bad := strings.Replace(string(backup), `"version": 1`, `"version": 99`, 1)
	if err := s.RestoreBackup([]byte(bad)); err == nil {
		t.Fatal("expected unsupported backup version rejection")
	}
}

func TestLegacyStateReceivesGatewayRouteTokensOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(CreateUserRequest{Name: "Legacy", InboundIDs: []string{"bermuda-vless-ws"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range u.Configs {
		u.Configs[i].RouteToken = ""
	}
	s.state.Users[u.UserID] = u
	if err := s.persistLocked(); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	items := loaded.GatewayRoutes()
	if len(items) != 1 || items[0].RouteToken == "" {
		t.Fatalf("legacy config did not receive a route token: %+v", items)
	}
}
