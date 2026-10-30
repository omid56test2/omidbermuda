package managed

import "time"

type Inbound struct {
	InboundID   string    `json:"inbound_id"`
	Name        string    `json:"name"`
	Tag         string    `json:"tag"`
	Protocol    string    `json:"protocol"`
	Transport   string    `json:"transport"`
	Listen      string    `json:"listen"`
	Port        int       `json:"port"`
	Path        string    `json:"path"`
	Mode        string    `json:"mode,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	ALPN        string    `json:"alpn"`
	Security    string    `json:"security"`
	Active      bool      `json:"active"`
	Builtin     bool      `json:"builtin"`
	CreatedAt   time.Time `json:"created_at"`
}

type UserConfig struct {
	ConfigID   string    `json:"config_id"`
	InboundID  string    `json:"inbound_id"`
	Protocol   string    `json:"protocol"`
	Secret     string    `json:"secret,omitempty"`
	RouteToken string    `json:"route_token,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type User struct {
	UserID          string       `json:"user_id"`
	Name            string       `json:"name"`
	SubToken        string       `json:"sub_token"`
	SubID           string       `json:"sub_id,omitempty"`
	ExpiresAt       *time.Time   `json:"expires_at,omitempty"`
	LimitBytes      uint64       `json:"limit_bytes"`
	UsedBytes       uint64       `json:"used_bytes"`
	UsageUpdatedAt  *time.Time   `json:"usage_updated_at,omitempty"`
	IPLimit         int          `json:"ip_limit"`
	ConnectionLimit int          `json:"connection_limit"`
	SpeedLimitBytes uint64       `json:"speed_limit_bytes"`
	Active          bool         `json:"active"`
	InboundIDs      []string     `json:"inbound_ids"`
	Configs         []UserConfig `json:"configs"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Group struct {
	SubID     string    `json:"sub_id"`
	Name      string    `json:"name"`
	Desc      string    `json:"desc,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type State struct {
	Users  map[string]User  `json:"users"`
	Groups map[string]Group `json:"groups"`
}

type UserView struct {
	User
	Expired     bool   `json:"expired"`
	Status      string `json:"status"`
	ConfigCount int    `json:"config_count"`
	GroupName   string `json:"group_name,omitempty"`
	SubURL      string `json:"sub_url"`
}

// GatewayRoute exposes the minimal runtime routing policy for a single user config.
type GatewayRoute struct {
	UserID          string `json:"user_id"`
	ConfigID        string `json:"config_id"`
	RouteToken      string `json:"route_token"`
	UpstreamPath    string `json:"upstream_path"`
	Listen          string `json:"listen"`
	Port            int    `json:"port"`
	IPLimit         int    `json:"ip_limit"`
	ConnectionLimit int    `json:"connection_limit"`
	SpeedLimitBytes uint64 `json:"speed_limit_bytes"`
}
