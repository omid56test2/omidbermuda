package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Snapshot struct {
	Running         bool      `json:"running"`
	PID             int       `json:"pid,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	LastExit        string    `json:"last_exit,omitempty"`
	UnexpectedExits int       `json:"unexpected_exits"`
	DesiredRunning  bool      `json:"desired_running"`
}
type Supervisor struct {
	mu             sync.RWMutex
	bin, config    string
	cmd            *exec.Cmd
	state          Snapshot
	stopping       bool
	starting       bool
	desiredRunning bool
}

func New(bin, config string) *Supervisor { return &Supervisor{bin: bin, config: config} }
func (s *Supervisor) Snapshot() Snapshot { s.mu.RLock(); defer s.mu.RUnlock(); return s.state }
func (s *Supervisor) validateConfig() error {
	if s.bin == "" {
		return errors.New("XRAY_BIN is not configured")
	}
	if _, err := os.Stat(s.bin); err != nil {
		return fmt.Errorf("xray binary: %w", err)
	}
	if _, err := os.Stat(s.config); err != nil {
		return fmt.Errorf("xray config: %w", err)
	}
	// Validate the generated configuration before stopping/restarting any
	// currently running process. Xray supports `run -test` for this purpose.
	validateCtx, cancelValidate := context.WithTimeout(context.Background(), 10*time.Second)
	validateCmd := exec.CommandContext(validateCtx, s.bin, "run", "-test", "-config", s.config)
	validateOutput, validateErr := validateCmd.CombinedOutput()
	validateCtxErr := validateCtx.Err()
	cancelValidate()
	if validateErr != nil {
		if validateCtxErr != nil {
			return fmt.Errorf("validate Xray config: %w", validateCtxErr)
		}
		detail := strings.TrimSpace(string(validateOutput))
		if detail != "" {
			return fmt.Errorf("validate Xray config: %w: %s", validateErr, detail)
		}
		return fmt.Errorf("validate Xray config: %w", validateErr)
	}
	return nil
}
func (s *Supervisor) Start() error {
	s.mu.Lock()
	if s.state.Running || s.starting {
		s.mu.Unlock()
		return nil
	}
	s.starting = true
	s.stopping = false
	s.mu.Unlock()
	started := false
	defer func() {
		if !started {
			s.mu.Lock()
			s.starting = false
			s.mu.Unlock()
		}
	}()
	if err := s.validateConfig(); err != nil {
		return err
	}
	// Xray runs under this supervisor until an explicit stop or process exit.
	cmd := exec.Command(s.bin, "run", "-config", s.config)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := configureProcess(cmd); err != nil {
		return fmt.Errorf("configure xray process: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xray: %w", err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.starting = false
	s.state.Running = true
	s.state.PID = cmd.Process.Pid
	s.state.StartedAt = time.Now().UTC()
	s.state.LastExit = ""
	s.desiredRunning = true
	s.state.DesiredRunning = true
	s.mu.Unlock()
	started = true
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Running = false
		s.state.PID = 0
		if err != nil {
			s.state.LastExit = err.Error()
		} else {
			s.state.LastExit = "process exited"
		}
		if !s.stopping {
			s.state.UnexpectedExits++
		}
	}()
	return nil
}
func (s *Supervisor) Stop(grace time.Duration) error {
	s.mu.Lock()
	cmd := s.cmd
	s.stopping = true
	s.desiredRunning = false
	s.state.DesiredRunning = false
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := terminateProcess(cmd); err != nil {
		_ = cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		for {
			s.mu.RLock()
			running := s.state.Running
			s.mu.RUnlock()
			if !running {
				close(done)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	select {
	case <-done:
		return nil
	case <-time.After(grace):
		_ = cmd.Process.Kill()
		select {
		case <-done:
			return nil
		case <-time.After(2 * time.Second):
			return errors.New("xray did not exit after termination")
		}
	}
}

func (s *Supervisor) Restart(grace time.Duration) error {
	s.mu.RLock()
	running := s.state.Running
	s.mu.RUnlock()
	if !running {
		return nil
	}
	// Validate the replacement config while the current Xray process is still
	// serving. A bad update must not take down a known-good running process.
	if err := s.validateConfig(); err != nil {
		return err
	}
	if err := s.Stop(grace); err != nil {
		return err
	}
	return s.Start()
}

// ShouldRestart reports whether an unexpectedly exited process was previously
// desired to be running. An explicit Stop clears this desire and disables recovery.
func (s *Supervisor) ShouldRestart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.desiredRunning && !s.state.Running && !s.starting && s.state.UnexpectedExits > 0
}
