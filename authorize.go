// Push authorization against acmanager.
//
// Each relay container is dedicated to ONE licensed forum. acmanager holds that
// forum's TOFU-locked egress IP (captured on the forum's first /api/push/
// activate) and its active flag. Every /send is gated on two things:
//
//   1. push is active for the license, and
//   2. the request's observed source IP == the licensed egress IP.
//
// The result is cached short-TTL so this isn't a per-notification round-trip,
// with fail-to-last-known-good (never fail-open-to-any): if acmanager is briefly
// unreachable we keep the last good answer; with no cache at all we fall back to
// the provisioned bootstrap IP. A leaked relay pass reused from a different
// server is dropped here because its source IP won't match.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type authResult struct {
	Active    bool   `json:"active"`
	AllowedIP string `json:"allowed_ip"`
	// Tier data rides back with the answer (informational for now — device
	// caps stay soft until tiers are finalized; enforcement would live here).
	DeviceLimit *int `json:"device_limit"`
	Devices30d  *int `json:"devices_30d"`
}

var (
	acmanagerURL string
	relayID      string
	relaySecret  string
	bootstrapIP  string // FORUM_ALLOWED_IP — used only until/unless acmanager answers

	authMu     sync.Mutex
	authCache  *authResult
	authAt     time.Time
	authTTL    = 120 * time.Second
	authClient = &http.Client{Timeout: 5 * time.Second}
)

func initAuthorize() {
	acmanagerURL = strings.TrimSpace(os.Getenv("ACMANAGER_AUTHORIZE_URL"))
	relayID = strings.TrimSpace(os.Getenv("RELAY_ID"))
	relaySecret = strings.TrimSpace(os.Getenv("RELAY_SECRET"))
	bootstrapIP = strings.TrimSpace(os.Getenv("FORUM_ALLOWED_IP"))
}

// authorizeEnabled reports whether IP enforcement is configured. With neither an
// acmanager URL nor a bootstrap IP, enforcement is off (dev/legacy relays).
func authorizeEnabled() bool {
	return acmanagerURL != "" || bootstrapIP != ""
}

// currentAuth returns the effective {active, allowedIP}, cached for authTTL.
func currentAuth() authResult {
	authMu.Lock()
	defer authMu.Unlock()

	if authCache != nil && time.Since(authAt) < authTTL {
		return *authCache
	}

	if acmanagerURL != "" && relayID != "" && relaySecret != "" {
		if res, err := fetchAuth(); err == nil {
			authCache = res
			authAt = time.Now()
			return *res
		}
		// acmanager unreachable — keep last-known-good if we have one.
		if authCache != nil {
			return *authCache
		}
	}

	// No acmanager answer ever — fall back to the provisioned bootstrap IP.
	return authResult{Active: bootstrapIP != "", AllowedIP: bootstrapIP}
}

func fetchAuth() (*authResult, error) {
	// Usage since the last successful report rides along (see usage.go);
	// counters are committed only after acmanager acknowledged them.
	sends, tokens := snapshotUsage(maxTokensPerReport)
	payload := map[string]any{
		"relay_id":     relayID,
		"relay_secret": relaySecret,
	}
	if sends > 0 || len(tokens) > 0 {
		payload["usage"] = map[string]any{"sends": sends, "tokens": tokens}
	}
	body, _ := json.Marshal(payload)
	resp, err := authClient.Post(acmanagerURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authorize http %d", resp.StatusCode)
	}
	var out authResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	commitUsage(sends, tokens)
	return &out, nil
}

// clientIP extracts the forum's real egress IP. The relay sits behind Dokploy's
// Traefik, which sets X-Real-IP to the real TCP peer (overwriting any
// client-supplied value). The container only accepts connections from Traefik
// (Dokploy network isolation), so trusting this header is safe.
func clientIP(r *http.Request) string {
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" && net.ParseIP(xr) != nil {
		return xr
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if net.ParseIP(first) != nil {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// authorizeSend reports whether this request may be forwarded, plus a reason on
// refusal. Drops when push is inactive, the licensed IP is unset (forum hasn't
// activated), or the observed IP doesn't match the licensed one.
func authorizeSend(r *http.Request) (bool, string) {
	if !authorizeEnabled() {
		return true, "" // enforcement not configured (dev/legacy)
	}
	auth := currentAuth()
	if !auth.Active {
		return false, "push inactive"
	}
	if auth.AllowedIP == "" {
		return false, "no licensed IP (not activated)"
	}
	if clientIP(r) != auth.AllowedIP {
		return false, "ip not authorized"
	}
	return true, ""
}
