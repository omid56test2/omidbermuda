package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TrafficCounters are Xray's per-user cumulative counters returned by StatsService.
type TrafficCounters struct {
	Uplink   uint64
	Downlink uint64
}

type UsageApplyResult struct {
	UsersUpdated int    `json:"users_updated"`
	BytesAdded   uint64 `json:"bytes_added"`
	QuotaReached bool   `json:"quota_reached"`
}

// ApplyTrafficBatch maps Xray's user>>>email>>>traffic>>>{uplink,downlink}
// counters back to user/config IDs and persists the delta as one transaction.
func (s *Store) ApplyTrafficBatch(batch map[string]TrafficCounters) (UsageApplyResult, error) {
	var result UsageApplyResult
	if len(batch) == 0 {
		return result, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	originals := make(map[string]User)
	now := time.Now().UTC()
	for email, counters := range batch {
		if email == "" {
			continue
		}
		var userID string
		for id, user := range s.state.Users {
			matched := false
			for _, cfg := range user.Configs {
				if email == user.UserID+"."+cfg.ConfigID {
					matched = true
					break
				}
			}
			if matched {
				userID = id
				break
			}
		}
		if userID == "" {
			continue // Ignore unknown/stale Xray identities.
		}
		delta := saturatingAdd(counters.Uplink, counters.Downlink)
		if delta == 0 {
			continue
		}
		user := s.state.Users[userID]
		if _, ok := originals[userID]; !ok {
			originals[userID] = cloneUser(user)
		}
		wasBelowQuota := user.LimitBytes == 0 || user.UsedBytes < user.LimitBytes
		user.UsedBytes = saturatingAdd(user.UsedBytes, delta)
		user.UsageUpdatedAt = &now
		user.UpdatedAt = now
		if wasBelowQuota && user.LimitBytes > 0 && user.UsedBytes >= user.LimitBytes {
			result.QuotaReached = true
		}
		s.state.Users[userID] = user
		result.BytesAdded = saturatingAdd(result.BytesAdded, delta)
	}
	if len(originals) == 0 {
		return result, nil
	}
	if err := s.persistLocked(); err != nil {
		for id, user := range originals {
			s.state.Users[id] = user
		}
		return UsageApplyResult{}, fmt.Errorf("persist traffic usage: %w", err)
	}
	result.UsersUpdated = len(originals)
	return result, nil
}

// HasExhaustedQuota reports whether any active managed user has reached a
// configured quota. It is used to retry a failed config refresh after restart.
func (s *Store) HasExhaustedQuota() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.state.Users {
		if user.Active && user.LimitBytes > 0 && user.UsedBytes >= user.LimitBytes {
			return true
		}
	}
	return false
}

func saturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}

type xrayStatsResponse struct {
	Stat []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"stat"`
}

// ParseXrayStats parses the JSON emitted by `xray api statsquery`.
func ParseXrayStats(data []byte) (map[string]TrafficCounters, error) {
	var response xrayStatsResponse
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Xray stats response: %w", err)
	}
	out := make(map[string]TrafficCounters)
	for _, stat := range response.Stat {
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		var valueText string
		if len(stat.Value) == 0 {
			continue
		}
		if stat.Value[0] == '"' {
			if err := json.Unmarshal(stat.Value, &valueText); err != nil {
				continue
			}
		} else {
			valueText = string(stat.Value)
		}
		value, err := strconv.ParseUint(strings.TrimSpace(valueText), 10, 64)
		if err != nil {
			continue
		}
		counters := out[parts[1]]
		switch parts[3] {
		case "uplink":
			counters.Uplink = value
		case "downlink":
			counters.Downlink = value
		default:
			continue
		}
		out[parts[1]] = counters
	}
	return out, nil
}

type UsageCollectorSnapshot struct {
	Enabled          bool       `json:"enabled"`
	Source           string     `json:"source"`
	APIAddress       string     `json:"api_address"`
	IntervalSeconds  int        `json:"interval_seconds"`
	LastSuccess      *time.Time `json:"last_success,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	LastUsersUpdated int        `json:"last_users_updated"`
	LastBytesAdded   uint64     `json:"last_bytes_added"`
	QuotaReached     bool       `json:"quota_reached"`
}

const DefaultXrayStatsAPIAddress = "127.0.0.1:10085"

type UsageCollector struct {
	store            *Store
	xrayBin          string
	apiAddress       string
	interval         time.Duration
	timeout          time.Duration
	onQuota          func() error
	mu               sync.RWMutex
	snapshot         UsageCollectorSnapshot
	quotaRefreshDone bool
}

func NewUsageCollector(store *Store, xrayBin, apiAddress string, onQuota func() error) *UsageCollector {
	if apiAddress == "" {
		apiAddress = DefaultXrayStatsAPIAddress
	}
	interval := 15 * time.Second
	return &UsageCollector{
		store: store, xrayBin: xrayBin, apiAddress: apiAddress,
		interval: interval, timeout: 5 * time.Second, onQuota: onQuota,
		snapshot: UsageCollectorSnapshot{Enabled: strings.TrimSpace(xrayBin) != "", Source: "Xray StatsService", APIAddress: apiAddress, IntervalSeconds: int(interval.Seconds())},
	}
}

func (c *UsageCollector) Snapshot() UsageCollectorSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *UsageCollector) Run(ctx context.Context) {
	if !c.Snapshot().Enabled {
		return
	}
	if err := c.poll(ctx); err != nil {
		c.setError(err)
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.poll(ctx); err != nil {
				c.setError(err)
			}
		}
	}
}

func (c *UsageCollector) poll(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.xrayBin, "api", "statsquery", "--server="+c.apiAddress, "-pattern", "user>>>", "-reset")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("query Xray StatsService: %w", ctx.Err())
		}
		return fmt.Errorf("query Xray StatsService: %w: %s", err, strings.TrimSpace(string(output)))
	}
	counters, err := ParseXrayStats(output)
	if err != nil {
		return err
	}
	result, err := c.store.ApplyTrafficBatch(counters)
	if err != nil {
		return err
	}
	quotaRefreshNeeded := result.QuotaReached || (!c.quotaRefreshDone && c.store.HasExhaustedQuota())
	if quotaRefreshNeeded && c.onQuota != nil {
		if err := c.onQuota(); err != nil {
			c.quotaRefreshDone = false
			return fmt.Errorf("quota reached but Xray client list refresh failed: %w", err)
		}
		c.quotaRefreshDone = true
	} else if !c.store.HasExhaustedQuota() {
		c.quotaRefreshDone = true
	}
	now := time.Now().UTC()
	c.mu.Lock()
	c.snapshot.LastSuccess = &now
	c.snapshot.LastError = ""
	c.snapshot.LastUsersUpdated = result.UsersUpdated
	c.snapshot.LastBytesAdded = result.BytesAdded
	c.snapshot.QuotaReached = result.QuotaReached
	c.mu.Unlock()
	return nil
}

func (c *UsageCollector) setError(err error) {
	c.mu.Lock()
	c.snapshot.LastError = err.Error()
	c.mu.Unlock()
}
