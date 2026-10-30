package managed

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseXrayStats(t *testing.T) {
	input := []byte(`{"stat":[
		{"name":"user>>>user-a.cfg-a>>>traffic>>>uplink","value":"1234"},
		{"name":"user>>>user-a.cfg-a>>>traffic>>>downlink","value":5678},
		{"name":"inbound>>>vless-ws-in>>>traffic>>>uplink","value":"900"},
		{"name":"user>>>user-b.cfg-b>>>online","value":"1"}
	]}`)
	got, err := ParseXrayStats(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["user-a.cfg-a"].Uplink != 1234 || got["user-a.cfg-a"].Downlink != 5678 {
		t.Fatalf("unexpected parsed stats: %#v", got)
	}
	if _, err := ParseXrayStats([]byte("not-json")); err == nil {
		t.Fatal("expected malformed JSON to fail")
	}
}

func TestApplyTrafficBatchAndQuotaCutoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(CreateUserRequest{
		Name: "Quota test", SubToken: "quota-test", InboundIDs: []string{"bermuda-vless-ws"},
		LimitValue: 1, LimitUnit: "MB",
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := user.UserID + "." + user.Configs[0].ConfigID
	result, err := store.ApplyTrafficBatch(map[string]TrafficCounters{identity: {Uplink: 400_000, Downlink: 700_000}})
	if err != nil {
		t.Fatal(err)
	}
	if result.UsersUpdated != 1 || !result.QuotaReached || result.BytesAdded != 1_100_000 {
		t.Fatalf("unexpected result: %+v", result)
	}
	list := store.ListUsers("")
	if len(list) != 1 || list[0].Status != "exhausted" || list[0].UsedBytes != 1_100_000 || list[0].UsageUpdatedAt == nil {
		t.Fatalf("quota/usage was not recorded: %+v", list)
	}
	cfg, err := store.BuildXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds []struct {
			Settings struct {
				Clients []json.RawMessage `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, in := range parsed.Inbounds {
		if len(in.Settings.Clients) != 0 {
			t.Fatal("quota-exhausted user remained in Xray config")
		}
	}
	persisted, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ListUsers("")[0].UsedBytes != 1_100_000 {
		t.Fatal("usage did not survive restart")
	}
}

func TestXrayConfigEnablesLocalStatsService(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.BuildXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	api, ok := cfg["api"].(map[string]any)
	if !ok || api["listen"] != "127.0.0.1:10085" {
		t.Fatalf("StatsService API is not loopback-only: %#v", cfg["api"])
	}
	if _, ok := cfg["stats"]; !ok {
		t.Fatal("stats object is missing")
	}
}

func TestUsageCollectorPollWithLocalXrayCLIStub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable stub")
	}
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(CreateUserRequest{Name: "Collector test", InboundIDs: []string{"bermuda-vless-ws"}, LimitValue: 1, LimitUnit: "MB"})
	if err != nil {
		t.Fatal(err)
	}
	email := user.UserID + "." + user.Configs[0].ConfigID
	cli := filepath.Join(t.TempDir(), "xray-stub")
	payload := `{"stat":[{"name":"user>>>` + email + `>>>traffic>>>uplink","value":"400000"},{"name":"user>>>` + email + `>>>traffic>>>downlink","value":700000}]}`
	script := "#!/bin/sh\ncat <<'JSON'\n" + payload + "\nJSON\n"
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	quotaRefreshes := 0
	collector := NewUsageCollector(store, cli, DefaultXrayStatsAPIAddress, func() error { quotaRefreshes++; return nil })
	if err := collector.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.ListUsers("")[0].UsedBytes; got != 1_100_000 {
		t.Fatalf("collector stored %d bytes, expected 1100000", got)
	}
	if snap := collector.Snapshot(); snap.LastSuccess == nil || snap.LastBytesAdded != 1_100_000 || !snap.QuotaReached {
		t.Fatalf("collector snapshot not updated: %+v", snap)
	}
	if quotaRefreshes != 1 {
		t.Fatalf("quota refresh was called %d times, expected once", quotaRefreshes)
	}
}
