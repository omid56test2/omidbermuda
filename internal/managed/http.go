package managed

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
)

type HTTPHandler struct {
	Store           *Store
	AdminToken      string
	PublicBaseURL   string
	OnConfigChanged func() error
}

func (h *HTTPHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/inbounds", h.admin(http.HandlerFunc(h.listInbounds)))
	mux.HandleFunc("GET /sub/{token}", h.getSubscription)
	mux.HandleFunc("GET /sub/raw/{token}", h.getRawSubscription)
	mux.HandleFunc("GET /sub/base64/{token}", h.getBase64Subscription)
	mux.Handle("POST /api/inbounds", h.admin(http.HandlerFunc(h.readOnlyInbound)))
	mux.Handle("PATCH /api/inbounds/{id}", h.admin(http.HandlerFunc(h.readOnlyInbound)))
	mux.Handle("DELETE /api/inbounds/{id}", h.admin(http.HandlerFunc(h.readOnlyInbound)))
	mux.Handle("GET /api/users", h.admin(http.HandlerFunc(h.listUsers)))
	mux.Handle("POST /api/users", h.admin(http.HandlerFunc(h.createUser)))
	mux.Handle("PATCH /api/users/{id}", h.admin(http.HandlerFunc(h.updateUser)))
	mux.Handle("DELETE /api/users/{id}", h.admin(http.HandlerFunc(h.deleteUser)))
	mux.Handle("GET /api/groups", h.admin(http.HandlerFunc(h.listGroups)))
	mux.Handle("POST /api/groups", h.admin(http.HandlerFunc(h.createGroup)))
	mux.Handle("PATCH /api/groups/{id}", h.admin(http.HandlerFunc(h.updateGroup)))
	mux.Handle("DELETE /api/groups/{id}", h.admin(http.HandlerFunc(h.deleteGroup)))
	// Compatibility aliases used by the legacy OMID-IRAN dashboard.
	mux.Handle("GET /api/subs", h.admin(http.HandlerFunc(h.listGroups)))
	mux.Handle("POST /api/subs", h.admin(http.HandlerFunc(h.createGroup)))
	mux.Handle("PATCH /api/subs/{id}", h.admin(http.HandlerFunc(h.updateGroup)))
	mux.Handle("DELETE /api/subs/{id}", h.admin(http.HandlerFunc(h.deleteGroup)))
}
func (h *HTTPHandler) admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.AdminToken == "" || !constantTimeEqual(bearer(r), h.AdminToken) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func bearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if len(v) < 7 || !strings.EqualFold(v[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(v[7:])
}
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var x byte
	for i := range a {
		x |= a[i] ^ b[i]
	}
	return x == 0
}
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, 400, map[string]string{"detail": "Invalid JSON body: " + err.Error()})
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, 400, map[string]string{"detail": "Request must contain one JSON object"})
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error) {
	var ae *APIError
	if errors.As(err, &ae) {
		writeJSON(w, ae.Status, map[string]string{"detail": ae.Message})
		return
	}
	writeJSON(w, 500, map[string]string{"detail": "Internal server error"})
}

func (h *HTTPHandler) listInbounds(w http.ResponseWriter, r *http.Request) {
	counts := h.Store.InboundConfigCounts()
	items := h.Store.Inbounds()
	type row struct {
		Inbound
		ConfigCount int `json:"config_count"`
	}
	out := make([]row, 0, len(items))
	for _, in := range items {
		out = append(out, row{Inbound: in, ConfigCount: counts[in.InboundID]})
	}
	writeJSON(w, 200, map[string]any{"inbounds": out})
}
func (h *HTTPHandler) readOnlyInbound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 403, map[string]string{"detail": "Built-in Inbounds are read-only and cannot be added, edited, or deleted"})
}
func (h *HTTPHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"users": h.Store.ListUsers(h.PublicBaseURL)})
}
func (h *HTTPHandler) createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.SubID == "" {
		req.SubID = req.GroupID
	}
	u, err := h.Store.CreateUser(req)
	if err != nil {
		writeError(w, err)
		return
	}
	subURL := "/sub/" + u.SubToken
	if h.PublicBaseURL != "" {
		subURL = strings.TrimRight(h.PublicBaseURL, "/") + subURL
	}
	syncStatus := h.refreshXrayConfig()
	writeJSON(w, 201, map[string]any{"ok": true, "user_id": u.UserID, "name": u.Name, "sub_token": u.SubToken, "sub_id": u.SubID, "inbound_ids": u.InboundIDs, "config_count": len(u.Configs), "sub_url": subURL, "xray_sync": syncStatus})
}
func (h *HTTPHandler) updateUser(w http.ResponseWriter, r *http.Request) {
	var req UpdateUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	u, err := h.Store.UpdateUser(r.PathValue("id"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	syncStatus := h.refreshXrayConfig()
	writeJSON(w, 200, map[string]any{"ok": true, "user_id": u.UserID, "active": u.Active, "config_count": len(u.Configs), "policy_updated": req.ExpiresDays != nil || req.LimitValue != nil || req.IPLimit != nil || req.ConnectionLimit != nil || req.SpeedLimitValue != nil, "xray_sync": syncStatus})
}
func (h *HTTPHandler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteUser(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	syncStatus := h.refreshXrayConfig()
	writeJSON(w, 200, map[string]any{"ok": true, "deleted": r.PathValue("id"), "xray_sync": syncStatus})
}
func (h *HTTPHandler) listGroups(w http.ResponseWriter, r *http.Request) {
	groups := h.Store.ListGroups()
	key := "groups"
	if strings.HasSuffix(r.URL.Path, "/subs") {
		key = "subs"
	}
	writeJSON(w, 200, map[string]any{key: groups})
}
func (h *HTTPHandler) createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Desc string `json:"desc"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	g, err := h.Store.CreateGroup(req.Name, req.Desc)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "group": g, "sub_id": g.SubID})
}
func (h *HTTPHandler) updateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name *string `json:"name"`
		Desc *string `json:"desc"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	g, err := h.Store.UpdateGroup(r.PathValue("id"), req.Name, req.Desc)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "group": g})
}
func (h *HTTPHandler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Store.DeleteGroup(id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "deleted": id})
}

func (h *HTTPHandler) refreshXrayConfig() string {
	if h.OnConfigChanged == nil {
		return "not_configured"
	}
	if err := h.OnConfigChanged(); err != nil {
		return "error: " + err.Error()
	}
	return "updated"
}

func (h *HTTPHandler) subscriptionBaseURL(r *http.Request) string {
	if strings.TrimSpace(h.PublicBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(h.PublicBaseURL), "/")
	}
	scheme := "http"
	// Do not trust X-Forwarded-Proto from arbitrary clients. Behind TLS
	// ingress, set PUBLIC_BASE_URL explicitly to the canonical HTTPS origin.
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (h *HTTPHandler) getSubscription(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	sub, err := h.Store.BuildSubscription(token, h.subscriptionBaseURL(r))
	if err != nil {
		writeError(w, err)
		return
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") || r.URL.Query().Get("view") == "1" {
		body, e := SubscriptionHTML(sub, token)
		if e != nil {
			writeJSON(w, 500, map[string]string{"detail": "Could not render subscription page"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.WriteHeader(200)
		_, _ = w.Write(body)
		return
	}
	h.writeBase64Subscription(w, sub)
}

func (h *HTTPHandler) getBase64Subscription(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	sub, err := h.Store.BuildSubscription(token, h.subscriptionBaseURL(r))
	if err != nil {
		writeError(w, err)
		return
	}
	h.writeBase64Subscription(w, sub)
}

func (h *HTTPHandler) writeBase64Subscription(w http.ResponseWriter, sub Subscription) {
	encoded := base64.StdEncoding.EncodeToString([]byte(sub.Raw))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Profile-Title", "OMID-IRAN - "+sub.UserName)
	w.Header().Set("Profile-Update-Interval", "6")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, encoded)
}

func (h *HTTPHandler) getRawSubscription(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	sub, err := h.Store.BuildSubscription(token, h.subscriptionBaseURL(r))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	_, _ = io.WriteString(w, sub.Raw)
}

func StoreFromEnv() (*Store, error) { return NewStore(os.Getenv("DATA_FILE")) }
