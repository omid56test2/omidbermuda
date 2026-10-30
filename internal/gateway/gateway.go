package gateway

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"omid-iran-bermuda/internal/managed"
)

type route struct {
	prefix       string
	upstreamPath string
	proxy        *httputil.ReverseProxy
	policy       *userPolicy
}

type userPolicy struct {
	mu              sync.Mutex
	active          int
	ips             map[string]int
	ipLimit         int
	connectionLimit int
	limiter         *byteLimiter
}

type byteLimiter struct {
	mu       sync.Mutex
	rate     uint64
	nextTime time.Time
}

func newByteLimiter(rate uint64) *byteLimiter {
	if rate == 0 {
		return nil
	}
	return &byteLimiter{rate: rate}
}

// Wait schedules each chunk against a shared per-user byte clock, so parallel
// requests share one aggregate rate rather than receiving one limit each.
func (l *byteLimiter) Wait(n int) {
	if l == nil || n <= 0 {
		return
	}
	l.mu.Lock()
	now := time.Now()
	start := l.nextTime
	if start.Before(now) {
		start = now
	}
	duration := time.Duration(float64(n) / float64(l.rate) * float64(time.Second))
	if duration < time.Microsecond {
		duration = time.Microsecond
	}
	finish := start.Add(duration)
	l.nextTime = finish
	wait := time.Until(finish)
	l.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

type Gateway struct {
	store               *managed.Store
	mu                  sync.RWMutex
	routes              []route
	policies            map[string]*userPolicy
	trustedProxies      []*net.IPNet
	active              atomic.Int64
	rejectedIP          atomic.Uint64
	rejectedConnections atomic.Uint64
	bytesThrottled      atomic.Uint64
}

type Snapshot struct {
	ActiveStreams       int64  `json:"active_streams"`
	RejectedIP          uint64 `json:"rejected_ip"`
	RejectedConnections uint64 `json:"rejected_connections"`
	ThrottledBytes      uint64 `json:"throttled_bytes"`
	Routes              int    `json:"routes"`
}

// New remains available for static test/legacy path routing.
func New(inbounds []managed.Inbound) *Gateway {
	g := &Gateway{policies: map[string]*userPolicy{}}
	for _, in := range inbounds {
		if in.Active {
			g.routes = append(g.routes, route{prefix: strings.TrimRight(in.Path, "/"), upstreamPath: in.Path, proxy: makeProxy(in.Listen, in.Port, in.Path)})
		}
	}
	sort.Slice(g.routes, func(i, j int) bool { return len(g.routes[i].prefix) > len(g.routes[j].prefix) })
	return g
}

func NewManaged(store *managed.Store, trustedCIDRs string) *Gateway {
	g := &Gateway{store: store, policies: map[string]*userPolicy{}}
	for _, entry := range strings.Split(trustedCIDRs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err == nil {
			g.trustedProxies = append(g.trustedProxies, network)
		}
	}
	g.Refresh()
	return g
}

func makeProxy(host string, port int, upstreamPath string) *httputil.ReverseProxy {
	return makeProxyWithTransport(host, port, upstreamPath, newUpstreamTransport())
}

func newUpstreamTransport() *http.Transport {
	return &http.Transport{Proxy: nil, MaxIdleConns: 128, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 512, IdleConnTimeout: 90 * time.Second, DisableCompression: true}
}

func makeProxyWithTransport(host string, port int, upstreamPath string, transport *http.Transport) *httputil.ReverseProxy {
	target, err := url.Parse("http://" + net.JoinHostPort(host, strconvPort(port)))
	if err != nil {
		return nil
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		// Static routes preserve their path; managed routes rewrite the public
		// per-user prefix to Bermuda's fixed loopback listener path.
		proxyPath := r.URL.Path
		if proxyPath == "" {
			proxyPath = upstreamPath
		}
		r.URL.Path = upstreamPath
		if strings.HasPrefix(proxyPath, "/connect/") {
			parts := strings.SplitN(strings.TrimPrefix(proxyPath, "/connect/"), "/", 2)
			if len(parts) == 2 && parts[1] != "" {
				suffix := "/" + parts[1]
				if !strings.Contains(suffix, "..") {
					r.URL.Path += suffix
				}
			}
		}
	}
	proxy.FlushInterval = -1
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "upstream inbound unavailable", http.StatusBadGateway)
	}
	return proxy
}

func (g *Gateway) Refresh() {
	if g.store == nil {
		return
	}
	specs := g.store.GatewayRoutes()
	g.mu.Lock()
	defer g.mu.Unlock()
	oldPolicies := g.policies
	newPolicies := make(map[string]*userPolicy)
	newRoutes := make([]route, 0, len(specs))
	transports := make(map[string]*http.Transport)
	for _, spec := range specs {
		policy := newPolicies[spec.UserID]
		if policy == nil {
			policy = oldPolicies[spec.UserID]
		}
		if policy == nil {
			policy = &userPolicy{ips: map[string]int{}}
		}
		policy.mu.Lock()
		policy.ipLimit = spec.IPLimit
		policy.connectionLimit = spec.ConnectionLimit
		if policy.limiter == nil || policy.limiter.rate != spec.SpeedLimitBytes {
			policy.limiter = newByteLimiter(spec.SpeedLimitBytes)
		}
		policy.mu.Unlock()
		newPolicies[spec.UserID] = policy
		prefix := "/connect/" + spec.RouteToken
		key := net.JoinHostPort(spec.Listen, strconvPort(spec.Port))
		transport := transports[key]
		if transport == nil {
			transport = newUpstreamTransport()
			transports[key] = transport
		}
		newRoutes = append(newRoutes, route{prefix: prefix, upstreamPath: spec.UpstreamPath, proxy: makeProxyWithTransport(spec.Listen, spec.Port, spec.UpstreamPath, transport), policy: policy})
	}
	sort.Slice(newRoutes, func(i, j int) bool { return len(newRoutes[i].prefix) > len(newRoutes[j].prefix) })
	g.routes, g.policies = newRoutes, newPolicies
}

func (g *Gateway) Snapshot() Snapshot {
	g.mu.RLock()
	routes := len(g.routes)
	g.mu.RUnlock()
	return Snapshot{ActiveStreams: g.active.Load(), RejectedIP: g.rejectedIP.Load(), RejectedConnections: g.rejectedConnections.Load(), ThrottledBytes: g.bytesThrottled.Load(), Routes: routes}
}

func (g *Gateway) clientIP(r *http.Request) string {
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(remote)
	if ip == nil {
		return "unknown"
	}
	trusted := false
	for _, network := range g.trustedProxies {
		if network.Contains(ip) {
			trusted = true
			break
		}
	}
	if trusted {
		for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
			if candidate := net.ParseIP(strings.TrimSpace(r.Header.Get(header))); candidate != nil {
				return candidate.String()
			}
		}
		if forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ","); len(forwarded) > 0 {
			if candidate := net.ParseIP(strings.TrimSpace(forwarded[0])); candidate != nil {
				return candidate.String()
			}
		}
	}
	return ip.String()
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	var matched *route
	for i := range g.routes {
		item := &g.routes[i]
		if r.URL.Path == item.prefix || strings.HasPrefix(r.URL.Path, item.prefix+"/") {
			matched = item
			break
		}
	}
	g.mu.RUnlock()
	if matched == nil || matched.proxy == nil {
		http.NotFound(w, r)
		return
	}
	if matched.policy == nil {
		matched.proxy.ServeHTTP(w, r)
		return
	}
	ip := g.clientIP(r)
	p := matched.policy
	p.mu.Lock()
	if p.connectionLimit > 0 && p.active >= p.connectionLimit {
		p.mu.Unlock()
		g.rejectedConnections.Add(1)
		http.Error(w, "simultaneous connection limit reached", http.StatusTooManyRequests)
		return
	}
	if p.ipLimit > 0 && p.ips[ip] == 0 && len(p.ips) >= p.ipLimit {
		p.mu.Unlock()
		g.rejectedIP.Add(1)
		http.Error(w, "active IP limit reached", http.StatusTooManyRequests)
		return
	}
	p.active++
	p.ips[ip]++
	limiter := p.limiter
	p.mu.Unlock()
	g.active.Add(1)
	defer func() {
		p.mu.Lock()
		p.active--
		p.ips[ip]--
		if p.ips[ip] <= 0 {
			delete(p.ips, ip)
		}
		p.mu.Unlock()
		g.active.Add(-1)
	}()

	if limiter != nil {
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = &limitedReader{ReadCloser: r.Body, limiter: limiter, counted: &g.bytesThrottled}
		}
		w = &limitedResponseWriter{ResponseWriter: w, limiter: limiter, counted: &g.bytesThrottled}
	}
	matched.proxy.ServeHTTP(w, r)
}

type limitedReader struct {
	io.ReadCloser
	limiter *byteLimiter
	counted *atomic.Uint64
}

func (r *limitedReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.limiter.Wait(n)
		r.counted.Add(uint64(n))
	}
	return n, err
}

type limitedResponseWriter struct {
	http.ResponseWriter
	limiter *byteLimiter
	counted *atomic.Uint64
}

func (w *limitedResponseWriter) Write(p []byte) (int, error) {
	w.limiter.Wait(len(p))
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.counted.Add(uint64(n))
	}
	return n, err
}
func (w *limitedResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *limitedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, brw, err := h.Hijack()
	if err != nil {
		return nil, nil, err
	}
	_ = brw.Flush()
	var pending []byte
	if n := brw.Reader.Buffered(); n > 0 {
		pending = make([]byte, n)
		_, _ = io.ReadFull(brw.Reader, pending)
	}
	wrapped := &limitedConn{Conn: conn, limiter: w.limiter, counted: w.counted}
	reader := bufio.NewReader(io.MultiReader(bytes.NewReader(pending), wrapped))
	return wrapped, bufio.NewReadWriter(reader, bufio.NewWriter(wrapped)), nil
}
func (w *limitedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type limitedConn struct {
	net.Conn
	limiter *byteLimiter
	counted *atomic.Uint64
}

func (c *limitedConn) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	if n > 0 {
		c.limiter.Wait(n)
		c.counted.Add(uint64(n))
	}
	return n, e
}
func (c *limitedConn) Write(p []byte) (int, error) {
	c.limiter.Wait(len(p))
	n, e := c.Conn.Write(p)
	if n > 0 {
		c.counted.Add(uint64(n))
	}
	return n, e
}

func strconvPort(p int) string {
	if p == 0 {
		return "0"
	}
	var b [5]byte
	i := len(b)
	for p > 0 {
		i--
		b[i] = byte('0' + p%10)
		p /= 10
	}
	return string(b[i:])
}
