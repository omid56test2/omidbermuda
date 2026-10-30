package managed

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort = 443
	maxBytes    = uint64(1 << 60)
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

type Store struct {
	mu       sync.RWMutex
	path     string
	state    State
	inbounds []Inbound
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		path = "./data/omid_state.json"
	}
	s := &Store{path: path, state: State{Users: map[string]User{}, Groups: map[string]Group{}}, inbounds: fixedInbounds()}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func fixedInbounds() []Inbound {
	created := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	return []Inbound{
		{
			InboundID: "bermuda-vless-xhttp", Name: "VLESS · XHTTP", Tag: "vless-xhttp-in",
			Protocol: "vless", Transport: "xhttp", Listen: "127.0.0.1", Port: 18443,
			Path: "/api/v1/sync", Mode: "auto", Fingerprint: "chrome", ALPN: "h2,http/1.1",
			Security: "none", Active: true, Builtin: true, CreatedAt: created,
		},
		{
			InboundID: "bermuda-vless-ws", Name: "VLESS · WebSocket", Tag: "vless-ws-in",
			Protocol: "vless", Transport: "ws", Listen: "127.0.0.1", Port: 18444,
			Path: "/api/v1/live", Fingerprint: "chrome", ALPN: "http/1.1",
			Security: "none", Active: true, Builtin: true, CreatedAt: created,
		},
		{
			InboundID: "bermuda-trojan-ws", Name: "Trojan · WebSocket", Tag: "trojan-ws-in",
			Protocol: "trojan", Transport: "ws", Listen: "127.0.0.1", Port: 18445,
			Path: "/api/v1/gateway", Fingerprint: "chrome", ALPN: "http/1.1",
			Security: "none", Active: true, Builtin: true, CreatedAt: created,
		},
	}
}

func titleProtocol(v string) string {
	if v == "vmess" {
		return "VMess"
	}
	return strings.ToUpper(v[:1]) + v[1:]
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read state: %w", err)
	}
	if err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &s.state); err != nil {
			return fmt.Errorf("decode state file: %w", err)
		}
		if s.state.Users == nil {
			s.state.Users = map[string]User{}
		}
		if s.state.Groups == nil {
			s.state.Groups = map[string]Group{}
		}
		s.migrateLegacyInboundAssignmentsLocked()
	}
	if err := s.ensureRouteTokensLocked(); err != nil {
		return fmt.Errorf("initialize gateway route tokens: %w", err)
	}
	return s.persistLocked()
}

func (s *Store) migrateLegacyInboundAssignmentsLocked() {
	legacyMap := map[string]string{
		"builtin-vless-ws":              "bermuda-vless-ws",
		"builtin-vless-xhttp-packet-up": "bermuda-vless-xhttp",
		"builtin-vless-xhttp-stream-up": "bermuda-vless-xhttp",
		"builtin-trojan-ws":             "bermuda-trojan-ws",
	}
	for userID, user := range s.state.Users {
		byInbound := make(map[string]UserConfig)
		shared := make(map[string]string)
		for _, cfg := range user.Configs {
			id := cfg.InboundID
			if mapped, ok := legacyMap[id]; ok {
				id = mapped
			} else if strings.HasPrefix(id, "builtin-") {
				continue // VMess and non-Bermuda transport variants have no matching listener.
			}
			if cfg.Secret != "" && shared[cfg.Protocol] == "" {
				shared[cfg.Protocol] = cfg.Secret
			}
			cfg.InboundID = id
			if _, exists := byInbound[id]; !exists {
				byInbound[id] = cfg
			}
		}
		ids := make([]string, 0, len(byInbound))
		configs := make([]UserConfig, 0, len(byInbound))
		for _, in := range s.inbounds {
			if cfg, ok := byInbound[in.InboundID]; ok {
				if shared[cfg.Protocol] != "" {
					cfg.Secret = shared[cfg.Protocol]
				}
				ids = append(ids, in.InboundID)
				configs = append(configs, cfg)
			}
		}
		user.InboundIDs = ids
		user.Configs = configs
		s.state.Users[userID] = user
	}
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if dir, e := os.Open(filepath.Dir(s.path)); e == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *Store) Inbounds() []Inbound {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Inbound(nil), s.inbounds...)
}

func (s *Store) CreateUser(req CreateUserRequest) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return User{}, bad("User name is required")
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	groupID := strings.TrimSpace(req.SubID)
	if groupID == "" {
		groupID = strings.TrimSpace(req.GroupID)
	}
	if groupID != "" {
		if _, ok := s.state.Groups[groupID]; !ok {
			return User{}, bad("Select a valid group")
		}
	}
	inboundIDs, err := s.validateInboundIDs(req.InboundIDs)
	if err != nil {
		return User{}, err
	}
	limits, err := normalizeLimits(req.LimitValue, req.LimitUnit, req.IPLimit, req.SpeedLimitValue, req.SpeedLimitUnit, req.ExpiresDays)
	if req.ConnectionLimit < 0 || req.ConnectionLimit > 1000000 {
		return User{}, bad("Connection limit must be between 0 and 1000000")
	}
	if err != nil {
		return User{}, err
	}
	token := strings.TrimSpace(req.SubToken)
	if token == "" {
		token, err = randomToken(24)
		if err != nil {
			return User{}, err
		}
	}
	if !tokenPattern.MatchString(token) {
		return User{}, bad("Sub-token must be 3–32 characters (letters, numbers, - and _)")
	}
	for _, u := range s.state.Users {
		if u.SubToken == token {
			return User{}, conflict("This sub-token is already assigned to another user")
		}
	}
	id, err := randomID()
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	u := User{UserID: id, Name: name, SubToken: token, SubID: groupID, ExpiresAt: limits.expiresAt,
		LimitBytes: limits.limitBytes, IPLimit: limits.ipLimit, ConnectionLimit: req.ConnectionLimit, SpeedLimitBytes: limits.speedBytes,
		Active: true, InboundIDs: inboundIDs, CreatedAt: now, UpdatedAt: now, UsedBytes: 0}
	sharedSecrets := map[string]string{}
	for _, inboundID := range inboundIDs {
		inbound := s.findInbound(inboundID)
		secret := sharedSecrets[inbound.Protocol]
		if secret == "" {
			secret, err = credentialFor(inbound.Protocol)
			if err != nil {
				return User{}, err
			}
			sharedSecrets[inbound.Protocol] = secret
		}
		configID, e := randomID()
		if e != nil {
			return User{}, e
		}
		routeToken, e := randomToken(32)
		if e != nil {
			return User{}, e
		}
		u.Configs = append(u.Configs, UserConfig{ConfigID: configID, InboundID: inboundID, Protocol: inbound.Protocol, Secret: secret, RouteToken: routeToken, CreatedAt: now})
	}
	s.state.Users[id] = u
	if err := s.persistLocked(); err != nil {
		delete(s.state.Users, id)
		return User{}, fmt.Errorf("save user: %w", err)
	}
	return cloneUser(u), nil
}

type CreateUserRequest struct {
	Name            string   `json:"name"`
	SubToken        string   `json:"sub_token"`
	SubID           string   `json:"sub_id"`
	GroupID         string   `json:"group_id"`
	InboundIDs      []string `json:"inbound_ids"`
	ExpiresDays     int      `json:"expires_days"`
	LimitValue      float64  `json:"limit_value"`
	LimitUnit       string   `json:"limit_unit"`
	IPLimit         int      `json:"ip_limit"`
	ConnectionLimit int      `json:"connection_limit"`
	SpeedLimitValue float64  `json:"speed_limit_value"`
	SpeedLimitUnit  string   `json:"speed_limit_unit"`
}

type UpdateUserRequest struct {
	Name            *string   `json:"name"`
	SubToken        *string   `json:"sub_token"`
	SubID           *string   `json:"sub_id"`
	GroupID         *string   `json:"group_id"`
	InboundIDs      *[]string `json:"inbound_ids"`
	ExpiresDays     *int      `json:"expires_days"`
	LimitValue      *float64  `json:"limit_value"`
	LimitUnit       *string   `json:"limit_unit"`
	IPLimit         *int      `json:"ip_limit"`
	ConnectionLimit *int      `json:"connection_limit"`
	SpeedLimitValue *float64  `json:"speed_limit_value"`
	SpeedLimitUnit  *string   `json:"speed_limit_unit"`
	Active          *bool     `json:"active"`
	ResetUsage      bool      `json:"reset_usage"`
}

func (s *Store) UpdateUser(id string, req UpdateUserRequest) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[id]
	if !ok {
		return User{}, notFound("User not found")
	}
	original := cloneUser(u)
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return User{}, bad("User name is required")
		}
		if len([]rune(n)) > 60 {
			n = string([]rune(n)[:60])
		}
		u.Name = n
	}
	if req.SubToken != nil {
		token := strings.TrimSpace(*req.SubToken)
		if !tokenPattern.MatchString(token) {
			return User{}, bad("Sub-token must be 3–32 characters (letters, numbers, - and _)")
		}
		for uid, other := range s.state.Users {
			if uid != id && other.SubToken == token {
				return User{}, conflict("This sub-token is already assigned to another user")
			}
		}
		u.SubToken = token
	}
	newGroup := u.SubID
	if req.SubID != nil {
		newGroup = strings.TrimSpace(*req.SubID)
	} else if req.GroupID != nil {
		newGroup = strings.TrimSpace(*req.GroupID)
	}
	if newGroup != "" {
		if _, exists := s.state.Groups[newGroup]; !exists {
			return User{}, bad("Select a valid group")
		}
	}
	u.SubID = newGroup
	if req.Active != nil {
		u.Active = *req.Active
	}
	if req.ExpiresDays != nil {
		days := *req.ExpiresDays
		if days < 0 || days > 36500 {
			return User{}, bad("Expiry must be between 0 and 36500 days")
		}
		u.ExpiresAt = nil
		if days > 0 {
			t := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
			u.ExpiresAt = &t
		}
	}
	if req.LimitValue != nil {
		unit := "GB"
		if req.LimitUnit != nil {
			unit = *req.LimitUnit
		}
		lim, err := normalizeLimits(*req.LimitValue, unit, 0, 0, "MBIT", 0)
		if err != nil {
			return User{}, err
		}
		u.LimitBytes = lim.limitBytes
	}
	if req.IPLimit != nil {
		if *req.IPLimit < 0 || *req.IPLimit > 1000000 {
			return User{}, bad("IP limit must be between 0 and 1000000")
		}
		u.IPLimit = *req.IPLimit
	}
	if req.ConnectionLimit != nil {
		if *req.ConnectionLimit < 0 || *req.ConnectionLimit > 1000000 {
			return User{}, bad("Connection limit must be between 0 and 1000000")
		}
		u.ConnectionLimit = *req.ConnectionLimit
	}
	if req.SpeedLimitValue != nil {
		unit := "MBIT"
		if req.SpeedLimitUnit != nil {
			unit = *req.SpeedLimitUnit
		}
		lim, err := normalizeLimits(0, "GB", 0, *req.SpeedLimitValue, unit, 0)
		if err != nil {
			return User{}, err
		}
		u.SpeedLimitBytes = lim.speedBytes
	}
	if req.InboundIDs != nil {
		ids, err := s.validateInboundIDs(*req.InboundIDs)
		if err != nil {
			return User{}, err
		}
		old := map[string]UserConfig{}
		sharedSecrets := map[string]string{}
		for _, c := range u.Configs {
			old[c.InboundID] = c
			if c.Secret != "" && sharedSecrets[c.Protocol] == "" {
				sharedSecrets[c.Protocol] = c.Secret
			}
		}
		configs := make([]UserConfig, 0, len(ids))
		for _, inboundID := range ids {
			if c, exists := old[inboundID]; exists {
				configs = append(configs, c)
				continue
			}
			in := s.findInbound(inboundID)
			secret := sharedSecrets[in.Protocol]
			if secret == "" {
				var e error
				secret, e = credentialFor(in.Protocol)
				if e != nil {
					return User{}, e
				}
				sharedSecrets[in.Protocol] = secret
			}
			cid, e := randomID()
			if e != nil {
				return User{}, e
			}
			routeToken, e := randomToken(32)
			if e != nil {
				return User{}, e
			}
			configs = append(configs, UserConfig{ConfigID: cid, InboundID: inboundID, Protocol: in.Protocol, Secret: secret, RouteToken: routeToken, CreatedAt: time.Now().UTC()})
		}
		u.InboundIDs, u.Configs = ids, configs
	}
	if req.ResetUsage {
		u.UsedBytes = 0
		u.UsageUpdatedAt = nil
	}
	u.UpdatedAt = time.Now().UTC()
	s.state.Users[id] = u
	if err := s.persistLocked(); err != nil {
		s.state.Users[id] = original
		return User{}, fmt.Errorf("save user: %w", err)
	}
	return cloneUser(u), nil
}

func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original, ok := s.state.Users[id]
	if !ok {
		return notFound("User not found")
	}
	delete(s.state.Users, id)
	if err := s.persistLocked(); err != nil {
		s.state.Users[id] = original
		return err
	}
	return nil
}

func (s *Store) ListUsers(baseURL string) []UserView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UserView, 0, len(s.state.Users))
	for _, user := range s.state.Users {
		u := cloneUser(user)
		for i := range u.Configs {
			u.Configs[i].Secret = ""
			u.Configs[i].RouteToken = ""
		}
		expired := u.ExpiresAt != nil && !u.ExpiresAt.After(time.Now().UTC())
		status := "active"
		if expired {
			status = "expired"
		} else if !u.Active {
			status = "inactive"
		} else if u.LimitBytes > 0 && u.UsedBytes >= u.LimitBytes {
			status = "exhausted"
		}
		groupName := ""
		if g, ok := s.state.Groups[u.SubID]; ok {
			groupName = g.Name
		}
		subURL := "/sub/" + u.SubToken
		if baseURL != "" {
			subURL = strings.TrimRight(baseURL, "/") + subURL
		}
		out = append(out, UserView{User: u, Expired: expired, Status: status, ConfigCount: len(u.Configs), GroupName: groupName, SubURL: subURL})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) validateInboundIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, bad("Select at least one inbound")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		in := s.findInbound(id)
		if in == nil || !in.Active || !in.Builtin {
			return nil, bad("One or more selected inbounds are invalid or inactive")
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, bad("Select at least one active inbound")
	}
	return out, nil
}
func (s *Store) findInbound(id string) *Inbound {
	for i := range s.inbounds {
		if s.inbounds[i].InboundID == id {
			return &s.inbounds[i]
		}
	}
	return nil
}
func cloneUser(u User) User {
	u.InboundIDs = append([]string(nil), u.InboundIDs...)
	u.Configs = append([]UserConfig(nil), u.Configs...)
	if u.ExpiresAt != nil {
		t := *u.ExpiresAt
		u.ExpiresAt = &t
	}
	if u.UsageUpdatedAt != nil {
		t := *u.UsageUpdatedAt
		u.UsageUpdatedAt = &t
	}
	return u
}

type normalizedLimits struct {
	expiresAt  *time.Time
	limitBytes uint64
	ipLimit    int
	speedBytes uint64
}

func normalizeLimits(limitValue float64, limitUnit string, ipLimit int, speedValue float64, speedUnit string, expiresDays int) (normalizedLimits, error) {
	if expiresDays < 0 || expiresDays > 36500 {
		return normalizedLimits{}, bad("Expiry must be between 0 and 36500 days")
	}
	if math.IsNaN(limitValue) || math.IsInf(limitValue, 0) || limitValue < 0 {
		return normalizedLimits{}, bad("Traffic quota must be a finite non-negative number")
	}
	if ipLimit < 0 || ipLimit > 1000000 {
		return normalizedLimits{}, bad("IP limit must be between 0 and 1000000")
	}
	if math.IsNaN(speedValue) || math.IsInf(speedValue, 0) || speedValue < 0 {
		return normalizedLimits{}, bad("Speed limit must be a finite non-negative number")
	}
	lu := strings.ToUpper(strings.TrimSpace(limitUnit))
	if lu == "" {
		lu = "GB"
	}
	factor := float64(0)
	switch lu {
	case "GB":
		factor = float64(1 << 30)
	case "MB":
		factor = float64(1 << 20)
	case "KB":
		factor = float64(1 << 10)
	default:
		return normalizedLimits{}, bad("Unsupported traffic quota unit")
	}
	bytes := limitValue * factor
	if bytes > float64(maxBytes) {
		return normalizedLimits{}, bad("Traffic quota is too large")
	}
	su := strings.ToUpper(strings.TrimSpace(speedUnit))
	if su == "" {
		su = "MBIT"
	}
	speedFactor := float64(0)
	switch su {
	case "MBIT":
		speedFactor = float64(1<<20) / 8
	case "KB":
		speedFactor = float64(1 << 10)
	case "MB":
		speedFactor = float64(1 << 20)
	default:
		return normalizedLimits{}, bad("Unsupported speed unit")
	}
	speed := speedValue * speedFactor
	if speed > float64(maxBytes) {
		return normalizedLimits{}, bad("Speed limit is too large")
	}
	var exp *time.Time
	if expiresDays > 0 {
		t := time.Now().UTC().Add(time.Duration(expiresDays) * 24 * time.Hour)
		exp = &t
	}
	return normalizedLimits{expiresAt: exp, limitBytes: uint64(bytes), ipLimit: ipLimit, speedBytes: uint64(speed)}, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	out := make([]byte, n)
	for i, v := range b {
		out[i] = chars[int(v)%len(chars)]
	}
	return string(out), nil
}
func credentialFor(protocol string) (string, error) {
	if protocol == "trojan" {
		return randomToken(32)
	}
	return randomID()
}

func (s *Store) ListGroups() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Group, 0, len(s.state.Groups))
	for _, g := range s.state.Groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (s *Store) CreateGroup(name, desc string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, bad("Group name is required")
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	if len([]rune(desc)) > 200 {
		desc = string([]rune(desc)[:200])
	}
	id, err := randomID()
	if err != nil {
		return Group{}, err
	}
	g := Group{SubID: id, Name: name, Desc: strings.TrimSpace(desc), CreatedAt: time.Now().UTC()}
	s.state.Groups[id] = g
	if err := s.persistLocked(); err != nil {
		delete(s.state.Groups, id)
		return Group{}, err
	}
	return g, nil
}
func (s *Store) UpdateGroup(id string, name, desc *string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.state.Groups[id]
	if !ok {
		return Group{}, notFound("Group not found")
	}
	if name != nil {
		v := strings.TrimSpace(*name)
		if v == "" {
			return Group{}, bad("Group name is required")
		}
		if len([]rune(v)) > 60 {
			v = string([]rune(v)[:60])
		}
		g.Name = v
	}
	if desc != nil {
		v := strings.TrimSpace(*desc)
		if len([]rune(v)) > 200 {
			v = string([]rune(v)[:200])
		}
		g.Desc = v
	}
	old := s.state.Groups[id]
	s.state.Groups[id] = g
	if err := s.persistLocked(); err != nil {
		s.state.Groups[id] = old
		return Group{}, err
	}
	return g, nil
}
func (s *Store) DeleteGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Groups[id]; !ok {
		return notFound("Group not found")
	}
	for _, u := range s.state.Users {
		if u.SubID == id {
			return conflict("This group is assigned to one or more users. Move those users first.")
		}
	}
	old := s.state.Groups[id]
	delete(s.state.Groups, id)
	if err := s.persistLocked(); err != nil {
		s.state.Groups[id] = old
		return err
	}
	return nil
}
func (s *Store) InboundConfigCounts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for _, u := range s.state.Users {
		for _, c := range u.Configs {
			out[c.InboundID]++
		}
	}
	return out
}

func (s *Store) ensureRouteTokensLocked() error {
	seen := map[string]bool{}
	for id, user := range s.state.Users {
		for i := range user.Configs {
			token := user.Configs[i].RouteToken
			if token == "" || seen[token] {
				var err error
				token, err = randomToken(32)
				if err != nil {
					return err
				}
				user.Configs[i].RouteToken = token
			}
			seen[token] = true
		}
		s.state.Users[id] = user
	}
	return nil
}

func (s *Store) GatewayRoutes() []GatewayRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]GatewayRoute, 0)
	now := time.Now().UTC()
	for _, user := range s.state.Users {
		if !userEligible(user, now) {
			continue
		}
		for _, cfg := range user.Configs {
			in := s.findInbound(cfg.InboundID)
			if in == nil || !in.Active || cfg.RouteToken == "" {
				continue
			}
			out = append(out, GatewayRoute{UserID: user.UserID, ConfigID: cfg.ConfigID, RouteToken: cfg.RouteToken, UpstreamPath: in.Path, Listen: in.Listen, Port: in.Port, IPLimit: user.IPLimit, ConnectionLimit: user.ConnectionLimit, SpeedLimitBytes: user.SpeedLimitBytes})
		}
	}
	return out
}

type BackupEnvelope struct {
	Format    string    `json:"format"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	State     State     `json:"state"`
}

func (s *Store) ExportBackup() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copyState := State{Users: make(map[string]User, len(s.state.Users)), Groups: make(map[string]Group, len(s.state.Groups))}
	for k, v := range s.state.Users {
		copyState.Users[k] = cloneUser(v)
	}
	for k, v := range s.state.Groups {
		copyState.Groups[k] = v
	}
	return json.MarshalIndent(BackupEnvelope{Format: "omid-iran-bermuda-backup", Version: 1, CreatedAt: time.Now().UTC(), State: copyState}, "", "  ")
}

func (s *Store) RestoreBackup(data []byte) error {
	if len(data) == 0 || len(data) > 10<<20 {
		return bad("Backup must be between 1 byte and 10 MiB")
	}
	var envelope BackupEnvelope
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&envelope); err != nil {
		return bad("Invalid backup JSON: " + err.Error())
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return bad("Backup must contain exactly one JSON document")
	}
	if envelope.Format != "omid-iran-bermuda-backup" || envelope.Version != 1 {
		return bad("Unsupported backup format or version")
	}
	if envelope.State.Users == nil {
		envelope.State.Users = map[string]User{}
	}
	if envelope.State.Groups == nil {
		envelope.State.Groups = map[string]Group{}
	}
	seenTokens, seenRoutes, seenConfigIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for id, group := range envelope.State.Groups {
		if id == "" || group.SubID != id || strings.TrimSpace(group.Name) == "" {
			return bad("Backup contains an invalid group")
		}
	}
	for id, user := range envelope.State.Users {
		if id == "" || user.UserID != id || !tokenPattern.MatchString(user.SubToken) {
			return bad("Backup contains an invalid user identity")
		}
		if seenTokens[user.SubToken] {
			return bad("Backup contains duplicate subscription tokens")
		}
		seenTokens[user.SubToken] = true
		if user.SubID != "" {
			if _, ok := envelope.State.Groups[user.SubID]; !ok {
				return bad("Backup user references a missing group")
			}
		}
		if user.IPLimit < 0 || user.ConnectionLimit < 0 {
			return bad("Backup contains a negative user limit")
		}
		assigned := map[string]bool{}
		for _, inboundID := range user.InboundIDs {
			if s.findInbound(inboundID) == nil || assigned[inboundID] {
				return bad("Backup contains an invalid or duplicate inbound assignment")
			}
			assigned[inboundID] = true
		}
		configInbounds := map[string]bool{}
		for i := range user.Configs {
			cfg := user.Configs[i]
			in := s.findInbound(cfg.InboundID)
			if in == nil || cfg.Secret == "" || cfg.Protocol != in.Protocol || !assigned[cfg.InboundID] || configInbounds[cfg.InboundID] {
				return bad("Backup contains an unsupported, duplicate, or incomplete user config")
			}
			configInbounds[cfg.InboundID] = true
			if cfg.ConfigID == "" || seenConfigIDs[cfg.ConfigID] {
				return bad("Backup contains an empty or duplicate config ID")
			}
			seenConfigIDs[cfg.ConfigID] = true
			if !tokenPattern.MatchString(cfg.RouteToken) || seenRoutes[cfg.RouteToken] {
				return bad("Backup contains an invalid or duplicate gateway route token")
			}
			seenRoutes[cfg.RouteToken] = true
		}
		if len(user.Configs) == 0 || len(user.Configs) != len(user.InboundIDs) {
			return bad("Backup contains a user with incomplete inbound assignments")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state
	s.state = envelope.State
	if err := s.persistLocked(); err != nil {
		s.state = old
		return fmt.Errorf("persist restored backup: %w", err)
	}
	return nil
}
