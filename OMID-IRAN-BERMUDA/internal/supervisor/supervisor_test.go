package supervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSupervisorStartsStopsAndRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signal test requires Unix")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-xray")
	config := filepath.Join(dir, "xray.json")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = \"run\" ] && [ \"$2\" = \"-test\" ]; then exit 0; fi\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(bin, config)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Running {
		t.Fatal("expected supervisor to report running after start")
	}
	if err := s.Stop(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Running {
		t.Fatal("expected process to stop")
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Running {
		t.Fatal("expected process to restart")
	}
	if err := s.Stop(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Running {
		t.Fatal("expected restarted process to stop")
	}
}

func TestSupervisorMarksUnexpectedExitForRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signal test requires Unix")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-xray-exit")
	config := filepath.Join(dir, "xray.json")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = \"run\" ] && [ \"$2\" = \"-test\" ]; then exit 0; fi\nsleep 0.1\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(bin, config)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.ShouldRestart() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("unexpected exit was not marked for recovery: %+v", s.Snapshot())
}

func TestSupervisorRejectsInvalidConfigBeforeStartingProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script process test requires Unix")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-xray-invalid")
	config := filepath.Join(dir, "xray.json")
	marker := filepath.Join(dir, "started.marker")
	script := "#!/bin/sh\nif [ \"$2\" = \"-test\" ]; then echo invalid-config >&2; exit 23; fi\ntouch \"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(bin, config)
	if err := s.Start(); err == nil {
		t.Fatal("expected invalid config to be rejected")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("Xray process started despite failed config validation")
	}
	if s.Snapshot().Running {
		t.Fatal("supervisor must remain stopped after validation failure")
	}
}

func TestRestartKeepsRunningProcessWhenNewConfigFailsValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signal test requires Unix")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-xray-restart")
	config := filepath.Join(dir, "xray.json")
	script := "#!/bin/sh\nif [ \"$2\" = \"-test\" ]; then if grep -q invalid \"$4\"; then echo invalid-config >&2; exit 23; fi; exit 0; fi\nexec sleep 60\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(bin, config)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Restart(2 * time.Second); err == nil {
		t.Fatal("expected restart validation error")
	}
	if !s.Snapshot().Running {
		t.Fatal("current Xray process should remain running after invalid config update")
	}
	if err := s.Stop(2 * time.Second); err != nil {
		t.Fatal(err)
	}
}
