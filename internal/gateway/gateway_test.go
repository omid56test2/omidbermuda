package gateway

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"omid-iran-bermuda/internal/managed"
)

func TestGatewayForwardsOnlyKnownListenerPaths(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("forwarded"))
	}))
	defer backend.Close()
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	g := New([]managed.Inbound{{InboundID: "test", Listen: host, Port: port, Path: "/api/v1/live", Active: true}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/live?x=1", strings.NewReader("body-data"))
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || rec.Body.String() != "forwarded" {
		t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/v1/live" || gotMethod != http.MethodPost || gotBody != "body-data" {
		t.Fatalf("request was not forwarded correctly: path=%s method=%s body=%s", gotPath, gotMethod, gotBody)
	}

	unknown := httptest.NewRecorder()
	g.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/api/v1/not-configured", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown gateway path should be 404, got %d", unknown.Code)
	}
}

func TestManagedRouteRewritesPathAndEnforcesConcurrentLimit(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/live" {
			t.Errorf("unexpected rewritten path: %s", r.URL.Path)
		}
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	policy := &userPolicy{ips: map[string]int{}, connectionLimit: 1}
	g := &Gateway{routes: []route{{prefix: "/connect/route-secret", upstreamPath: "/api/v1/live", proxy: makeProxy(host, port, "/api/v1/live"), policy: policy}}, policies: map[string]*userPolicy{"user-1": policy}}
	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodGet, "/connect/route-secret", nil)
	firstReq.RemoteAddr = "198.51.100.10:1234"
	done := make(chan struct{})
	go func() { g.ServeHTTP(first, firstReq); close(done) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach backend")
	}
	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodGet, "/connect/route-secret", nil)
	secondReq.RemoteAddr = "198.51.100.11:1234"
	g.ServeHTTP(second, secondReq)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected concurrent limit 429, got %d", second.Code)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}
	if g.Snapshot().RejectedConnections != 1 {
		t.Fatalf("unexpected gateway snapshot: %+v", g.Snapshot())
	}
}

func TestIPLimitRejectsNewActiveAddress(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-release; w.WriteHeader(204) }))
	defer backend.Close()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	policy := &userPolicy{ips: map[string]int{}, ipLimit: 1}
	g := &Gateway{routes: []route{{prefix: "/connect/route", upstreamPath: "/api/v1/live", proxy: makeProxy(host, port, "/api/v1/live"), policy: policy}}}
	first := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/connect/route", nil)
	req.RemoteAddr = "198.51.100.20:1"
	done := make(chan struct{})
	go func() { g.ServeHTTP(first, req); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	second := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/connect/route", nil)
	req2.RemoteAddr = "198.51.100.21:2"
	g.ServeHTTP(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected IP limit 429, got %d", second.Code)
	}
	close(release)
	<-done
}

func TestSharedByteLimiterPacesConcurrentChunks(t *testing.T) {
	limiter := newByteLimiter(2000)
	limiter.Wait(1000)
	start := time.Now()
	limiter.Wait(1000)
	if elapsed := time.Since(start); elapsed < 350*time.Millisecond {
		t.Fatalf("expected shared limiter delay, got %s", elapsed)
	}
}

func TestManagedGatewayPreservesWebSocketUpgradeWithRateLimiter(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/live" {
			t.Errorf("websocket upstream path was not rewritten: %s", r.URL.Path)
		}
		h, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream does not support hijacking")
			return
		}
		conn, brw, err := h.Hijack()
		if err != nil {
			t.Errorf("upstream hijack: %v", err)
			return
		}
		_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = brw.Flush()
		_, _ = conn.Write([]byte("stage5-ok"))
		_ = conn.Close()
	}))
	defer backend.Close()
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	policy := &userPolicy{ips: map[string]int{}, limiter: newByteLimiter(1 << 20)}
	g := &Gateway{routes: []route{{prefix: "/connect/ws-token", upstreamPath: "/api/v1/live", proxy: makeProxy(host, port, "/api/v1/live"), policy: policy}}}
	server := httptest.NewServer(g)
	defer server.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(conn, "GET /connect/ws-token HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("expected protocol switch, got %q", status)
	}
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == "\r\n" {
			break
		}
	}
	payload := make([]byte, len("stage5-ok"))
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "stage5-ok" {
		t.Fatalf("unexpected websocket payload %q", payload)
	}
}
