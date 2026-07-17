// Usage metering, piggybacked on the authorize call.
//
// The relay is the one point every push must pass through, so it is the
// tamper-proof place to meter a forum's size: it counts sends and the distinct
// devices it delivered to. Only truncated SHA-256 hashes of device tokens are
// kept and reported — raw tokens never leave the send path, so what reaches
// acmanager is an anonymous device count, not identifiers.
//
// acmanager is the accumulator (this container restarts on every redeploy and
// keeps no state): we report deltas since the last successful report, and
// counters are cleared only after acmanager acknowledged them. Reporting rides
// inside the existing /api/push/authorize request — no extra schedule, no
// reporting while idle, and at most a few minutes of counts lost on restart.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

const (
	// maxTokensPerReport bounds one report's payload (~66KB of hex at 2000);
	// anything beyond rides along on subsequent reports.
	maxTokensPerReport = 2000
	// maxPendingTokens is a hard memory backstop (~3MB) — far above any real
	// forum's between-report churn.
	maxPendingTokens = 50000
)

var (
	usageMu     sync.Mutex
	usageSends  int64
	usageTokens = map[string]struct{}{}
)

// recordUsage counts one successfully delivered push.
func recordUsage(token string) {
	sum := sha256.Sum256([]byte(token))
	h := hex.EncodeToString(sum[:16])
	usageMu.Lock()
	usageSends++
	if len(usageTokens) < maxPendingTokens {
		usageTokens[h] = struct{}{}
	}
	usageMu.Unlock()
}

// snapshotUsage returns the send counter plus up to max pending token hashes
// WITHOUT clearing them — commitUsage removes them only after a successful
// report, so a failed authorize call loses nothing.
func snapshotUsage(max int) (int64, []string) {
	usageMu.Lock()
	defer usageMu.Unlock()
	tokens := make([]string, 0, max)
	for h := range usageTokens {
		if len(tokens) >= max {
			break
		}
		tokens = append(tokens, h)
	}
	return usageSends, tokens
}

// commitUsage clears counters that acmanager has acknowledged.
func commitUsage(sends int64, tokens []string) {
	usageMu.Lock()
	defer usageMu.Unlock()
	usageSends -= sends
	if usageSends < 0 {
		usageSends = 0
	}
	for _, h := range tokens {
		delete(usageTokens, h)
	}
}
