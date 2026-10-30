package managed

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SubscriptionConfig struct {
	ConfigID  string `json:"config_id"`
	InboundID string `json:"inbound_id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Transport string `json:"transport"`
	URI       string `json:"uri"`
}

type Subscription struct {
	UserName  string               `json:"user_name"`
	UpdatedAt time.Time            `json:"updated_at"`
	Configs   []SubscriptionConfig `json:"configs"`
	Raw       string               `json:"raw"`
}

func userEligible(u User, now time.Time) bool {
	if !u.Active {
		return false
	}
	if u.ExpiresAt != nil && !u.ExpiresAt.After(now) {
		return false
	}
	if u.LimitBytes > 0 && u.UsedBytes >= u.LimitBytes {
		return false
	}
	return true
}

func validateBermudaInbounds(inbounds []Inbound) error {
	seenPorts := map[int]bool{}
	seenPaths := map[string]bool{}
	seenTags := map[string]bool{}
	for _, in := range inbounds {
		ip := net.ParseIP(in.Listen)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("inbound %s must listen on a loopback IP", in.InboundID)
		}
		if in.Port < 1 || in.Port > 65535 || seenPorts[in.Port] {
			return fmt.Errorf("inbound %s has an invalid or duplicate port", in.InboundID)
		}
		if in.Tag == "" || seenTags[in.Tag] {
			return fmt.Errorf("inbound %s has an empty or duplicate Xray tag", in.InboundID)
		}
		if !strings.HasPrefix(in.Path, "/api/v1/") || strings.Contains(in.Path, "..") || seenPaths[in.Path] {
			return fmt.Errorf("inbound %s has an invalid or duplicate Gateway path", in.InboundID)
		}
		if !((in.Protocol == "vless" && (in.Transport == "ws" || in.Transport == "xhttp")) || (in.Protocol == "trojan" && in.Transport == "ws")) {
			return fmt.Errorf("inbound %s has an unsupported protocol/transport combination", in.InboundID)
		}
		seenPorts[in.Port], seenPaths[in.Path], seenTags[in.Tag] = true, true, true
	}
	return nil
}

// BuildXrayConfig creates the actual three-listener Bermuda-style Xray config.
// Xray listeners remain loopback-only; the public HTTP Gateway forwards only
// the matching, explicitly configured paths to these local listeners.
func (s *Store) BuildXrayConfig() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateBermudaInbounds(s.inbounds); err != nil {
		return nil, err
	}
	clientsByInbound := make(map[string][]map[string]any, len(s.inbounds))
	now := time.Now().UTC()
	for _, u := range s.state.Users {
		if !userEligible(u, now) {
			continue
		}
		for _, c := range u.Configs {
			if c.Secret == "" {
				continue
			}
			in := s.findInbound(c.InboundID)
			if in == nil || !in.Active {
				continue
			}
			client := map[string]any{"email": u.UserID + "." + c.ConfigID, "level": 0}
			if in.Protocol == "trojan" {
				client["password"] = c.Secret
			} else {
				client["id"] = c.Secret
			}
			clientsByInbound[in.InboundID] = append(clientsByInbound[in.InboundID], client)
		}
	}

	inbounds := make([]map[string]any, 0, len(s.inbounds))
	for _, in := range s.inbounds {
		if !in.Active {
			continue
		}
		settings := map[string]any{"clients": clientsByInbound[in.InboundID]}
		if settings["clients"] == nil {
			settings["clients"] = []map[string]any{}
		}
		if in.Protocol == "vless" {
			settings["decryption"] = "none"
		}
		stream := map[string]any{"network": in.Transport, "security": "none"}
		if in.Transport == "ws" {
			stream["wsSettings"] = map[string]any{"path": in.Path, "heartbeatPeriod": 30}
		} else if in.Transport == "xhttp" {
			stream["xhttpSettings"] = map[string]any{"path": in.Path, "mode": in.Mode}
		} else {
			return nil, fmt.Errorf("unsupported transport %q for inbound %s", in.Transport, in.InboundID)
		}
		inbounds = append(inbounds, map[string]any{
			"tag": in.Tag, "listen": in.Listen, "port": in.Port,
			"protocol": in.Protocol, "settings": settings, "streamSettings": stream,
		})
	}

	// The Xray StatsService is bound to loopback only. Traffic counters are
	// queried locally by the Go usage collector; this API is never exposed by
	// the public Gateway.
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"api": map[string]any{
			"tag": "api", "listen": DefaultXrayStatsAPIAddress, "services": []string{"StatsService"},
		},
		"stats": map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{"0": map[string]any{
				"statsUserUplink": true, "statsUserDownlink": true, "statsUserOnline": true,
			}},
		},
		"inbounds": inbounds,
		"outbounds": []map[string]any{
			{"tag": "direct", "protocol": "freedom", "settings": map[string]any{}},
			{"tag": "block", "protocol": "blackhole", "settings": map[string]any{"response": map[string]any{"type": "none"}}},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func (s *Store) WriteXrayConfig(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("XRAY_CONFIG path is empty")
	}
	data, err := s.BuildXrayConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create Xray config directory: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err == nil {
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
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) BuildSubscription(token, baseURL string) (Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var user User
	found := false
	for _, u := range s.state.Users {
		if u.SubToken == token {
			user, found = cloneUser(u), true
			break
		}
	}
	if !found {
		return Subscription{}, notFound("Subscription not found")
	}
	if !userEligible(user, time.Now().UTC()) {
		return Subscription{}, forbidden("Subscription is inactive, expired, or exhausted")
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Hostname() == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return Subscription{}, bad("A valid PUBLIC_BASE_URL is required to build client configurations")
	}
	host := base.Hostname()
	port := base.Port()
	if port == "" {
		if base.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	address := net.JoinHostPort(host, port)
	security := "none"
	if base.Scheme == "https" {
		security = "tls"
	}

	byID := make(map[string]Inbound, len(s.inbounds))
	for _, in := range s.inbounds {
		byID[in.InboundID] = in
	}
	configs := make([]SubscriptionConfig, 0, len(user.Configs))
	for _, c := range user.Configs {
		in, ok := byID[c.InboundID]
		if !ok || !in.Active || c.Secret == "" {
			continue
		}
		q := url.Values{}
		q.Set("security", security)
		if security == "tls" {
			q.Set("sni", host)
			q.Set("fp", in.Fingerprint)
			q.Set("alpn", in.ALPN)
		}
		var link string
		if in.Protocol == "vless" {
			q.Set("encryption", "none")
			if in.Transport == "ws" {
				q.Set("type", "ws")
				q.Set("host", host)
				q.Set("path", "/connect/"+c.RouteToken)
			} else {
				q.Set("type", "xhttp")
				q.Set("mode", in.Mode)
				q.Set("path", "/connect/"+c.RouteToken)
			}
			u := url.URL{Scheme: "vless", User: url.User(c.Secret), Host: address, RawQuery: q.Encode(), Fragment: user.Name + " - " + in.Name}
			link = u.String()
		} else if in.Protocol == "trojan" {
			q.Set("type", "ws")
			q.Set("host", host)
			q.Set("path", "/connect/"+c.RouteToken)
			u := url.URL{Scheme: "trojan", User: url.User(c.Secret), Host: address, RawQuery: q.Encode(), Fragment: user.Name + " - " + in.Name}
			link = u.String()
		} else {
			continue
		}
		configs = append(configs, SubscriptionConfig{ConfigID: c.ConfigID, InboundID: in.InboundID, Name: in.Name, Protocol: in.Protocol, Transport: in.Transport, URI: link})
	}
	if len(configs) == 0 {
		return Subscription{}, bad("No active client configurations are assigned to this user")
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].InboundID < configs[j].InboundID })
	lines := make([]string, 0, len(configs))
	for _, c := range configs {
		lines = append(lines, c.URI)
	}
	return Subscription{UserName: user.Name, UpdatedAt: time.Now().UTC(), Configs: configs, Raw: strings.Join(lines, "\n")}, nil
}

func SubscriptionHTML(sub Subscription, token string) ([]byte, error) {
	const page = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>اشتراک {{.UserName}} | OMID-IRAN</title><style>
:root{color-scheme:dark;font-family:Tahoma,Arial,sans-serif;background:#0b1020;color:#edf2ff}*{box-sizing:border-box}body{margin:0;padding:24px;background:radial-gradient(circle at 20% 0,#1b3151,#0b1020 48%)}main{max-width:900px;margin:auto}.hero,.item{border:1px solid #293b58;background:#111b2d;border-radius:18px;padding:20px;margin-bottom:14px}.muted{color:#9cabc2;font-size:13px}.item h3{margin:0 0 9px}.row{display:flex;gap:10px;align-items:center;justify-content:space-between;flex-wrap:wrap}.pill{font-size:12px;background:#1c3550;border-radius:30px;padding:6px 10px;color:#a9d8ff}button,a.btn{border:0;border-radius:10px;padding:10px 14px;background:#2563eb;color:white;text-decoration:none;cursor:pointer;font-size:13px}.secondary{background:#26354c}.uri{width:100%;margin-top:12px;border-radius:10px;padding:11px;background:#08111f;color:#a8d3ff;border:1px solid #30415b;direction:ltr;text-align:left;overflow-wrap:anywhere;font:12px Consolas,monospace}.qr{display:inline-block;background:white;padding:12px;border-radius:12px;margin-top:12px;max-width:100%}.hidden{display:none!important}.foot{padding:12px;color:#8596ad;text-align:center;font-size:12px}</style></head><body><main><section class="hero"><div class="row"><div><div class="muted">OMID-IRAN · SUBSCRIPTION</div><h1 style="margin:8px 0">اشتراک {{.UserName}}</h1><div class="muted">{{len .Configs}} کانفیگ فعال · به‌روزرسانی خودکار از طریق لینک اشتراک</div></div><div class="row"><button onclick="copyAll()">کپی همه کانفیگ‌ها</button><a class="btn secondary" href="/sub/base64/{{.Token}}">دریافت Base64</a></div></div></section>{{range .Configs}}<section class="item"><div class="row"><div><h3>{{.Name}}</h3><div class="muted">{{.Protocol}} · {{.Transport}}</div></div><span class="pill">فعال</span></div><div class="uri" id="uri-{{.ConfigID}}">{{.URI}}</div><div class="row" style="margin-top:12px"><button onclick="copyOne('uri-{{.ConfigID}}')">کپی کانفیگ</button><a class="btn secondary" href="{{.URI}}">بازکردن در کلاینت</a><button class="secondary" onclick="showQR('qr-{{.ConfigID}}','uri-{{.ConfigID}}')">نمایش QR</button></div><div class="qr hidden" id="qr-{{.ConfigID}}"></div></section>{{end}}<div class="foot">این لینک خصوصی است؛ آن را با دیگران به اشتراک نگذارید.</div></main><script src="/assets/bermuda-qr.js"></script><script>async function copyOne(id){const t=document.getElementById(id).textContent;await navigator.clipboard.writeText(t)}async function copyAll(){const a=[...document.querySelectorAll('.uri')].map(x=>x.textContent).join('\n');await navigator.clipboard.writeText(a)}function showQR(qid,uid){const box=document.getElementById(qid),value=document.getElementById(uid)?.textContent||'';if(!box||!value)return;if(box.classList.contains('hidden')){const res=window.BermudaQR?.toSVG(value);if(!res||res.error){box.textContent='ساخت QR ممکن نشد';}else{box.innerHTML=res.svg;}box.classList.remove('hidden')}else{box.classList.add('hidden')}} </script></body></html>`
	tmpl, err := template.New("subscription").Parse(page)
	if err != nil {
		return nil, err
	}
	// Token is attached by HTTPHandler through the template data wrapper.
	var b strings.Builder
	type viewConfig struct {
		ConfigID  string
		Name      string
		Protocol  string
		Transport string
		URI       template.URL
	}
	viewConfigs := make([]viewConfig, 0, len(sub.Configs))
	for _, c := range sub.Configs {
		viewConfigs = append(viewConfigs, viewConfig{ConfigID: c.ConfigID, Name: c.Name, Protocol: c.Protocol, Transport: c.Transport, URI: template.URL(c.URI)})
	}
	data := struct {
		UserName string
		Configs  []viewConfig
		Token    string
	}{UserName: sub.UserName, Configs: viewConfigs, Token: token}
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
