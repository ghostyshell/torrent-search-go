package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	pkgmodels "torrent-search-go/pkg/models"
	"torrent-search-go/pkg/storage"
)

// DDoSLimit is one sliding-window auto-block rule.
type DDoSLimit struct {
	Name      string
	Window    time.Duration
	Threshold int
}

// DDoSConfig controls abuse detection thresholds.
type DDoSConfig struct {
	Limits       []DDoSLimit
	SyncInterval time.Duration
}

func DefaultDDoSConfig() DDoSConfig {
	return DDoSConfig{
		Limits: []DDoSLimit{
			{Name: "1m", Window: time.Minute, Threshold: 1200},
			{Name: "1h", Window: time.Hour, Threshold: 8000},
			{Name: "6h", Window: 6 * time.Hour, Threshold: 30000},
			{Name: "1d", Window: 24 * time.Hour, Threshold: 80000},
			{Name: "1w", Window: 7 * 24 * time.Hour, Threshold: 300000},
			{Name: "1mo", Window: 30 * 24 * time.Hour, Threshold: 800000},
		},
		SyncInterval: 60 * time.Second,
	}
}

// IPEntry tracks per-IP request timestamps (retained up to the longest window).
type IPEntry struct {
	times []time.Time
}

// DDoSGuard tracks per-IP traffic and enforces the blocklist.
type DDoSGuard struct {
	cfg          DDoSConfig
	db           storage.Database
	skipToken    string
	maxRetention time.Duration
	mu           sync.RWMutex
	traffic      map[string]*IPEntry
	blocked      map[string]bool
}

// NewDDoSGuard creates a guard and immediately syncs the blocklist from MongoDB.
func NewDDoSGuard(db storage.Database, cfg DDoSConfig, addonAPIToken string) *DDoSGuard {
	maxRetention := time.Duration(0)
	for _, lim := range cfg.Limits {
		if lim.Window > maxRetention {
			maxRetention = lim.Window
		}
	}
	if maxRetention <= 0 {
		maxRetention = 30 * 24 * time.Hour
	}
	g := &DDoSGuard{
		cfg:          cfg,
		db:           db,
		skipToken:    addonAPIToken,
		maxRetention: maxRetention,
		traffic:      make(map[string]*IPEntry),
		blocked:      make(map[string]bool),
	}
	g.syncBlocklist()
	return g
}

// Start begins background sync of the blocklist. Call once after creation.
func (g *DDoSGuard) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(g.cfg.SyncInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				g.syncBlocklist()
				g.sweepTraffic()
			}
		}
	}()
}

// sweepTraffic deletes traffic entries whose pruned times slice is empty.
// Without this the traffic map grows monotonically with the set of distinct
// client IPs seen - an unbounded memory leak on a public-facing API.
func (g *DDoSGuard) sweepTraffic() {
	cutoff := time.Now().Add(-g.maxRetention)
	g.mu.Lock()
	defer g.mu.Unlock()
	for ip, e := range g.traffic {
		pruned := pruneTimes(e.times, cutoff)
		if len(pruned) == 0 {
			delete(g.traffic, ip)
			continue
		}
		e.times = pruned
	}
}

func (g *DDoSGuard) syncBlocklist() {
	ips, err := g.db.GetBlockedIPs(context.Background())
	if err != nil {
		log.Printf("[ddos] blocklist sync error: %v", err)
		return
	}
	g.mu.Lock()
	g.blocked = make(map[string]bool, len(ips))
	for _, b := range ips {
		g.blocked[b.IP] = true
	}
	g.mu.Unlock()
}

func ddosSkipRequest(r *http.Request, addonToken string) bool {
	path := r.URL.Path
	if path == "/health" || strings.HasPrefix(path, "/health/") {
		return true
	}
	if strings.HasPrefix(path, "/stremio/") || strings.HasPrefix(path, "/magnetio/") {
		return true
	}
	return MatchesAddonToken(r, addonToken)
}

func (g *DDoSGuard) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ddosSkipRequest(r, g.skipToken) {
				next.ServeHTTP(w, r)
				return
			}

			ip := clientIP(r)
			if ip == "" {
				next.ServeHTTP(w, r)
				return
			}

			g.mu.RLock()
			isBlocked := g.blocked[ip]
			g.mu.RUnlock()

			if isBlocked {
				writeIPBlocked(w, "Access denied: your IP has been blocked due to suspicious activity.")
				return
			}

			if lim, count, tripped := g.recordAndCheck(ip); tripped {
				g.autoBlock(ip, lim, int64(count))
				writeIPBlocked(w, "Access denied: rate limit exceeded.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeIPBlocked(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   msg,
		"code":    "IP_BLOCKED",
	})
}

func countSince(times []time.Time, since time.Time) int {
	n := 0
	for _, t := range times {
		if t.After(since) {
			n++
		}
	}
	return n
}

func pruneTimes(times []time.Time, cutoff time.Time) []time.Time {
	j := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[j] = t
			j++
		}
	}
	return times[:j]
}

func checkDDoSLimits(times []time.Time, now time.Time, limits []DDoSLimit) (DDoSLimit, int, bool) {
	for _, lim := range limits {
		if c := countSince(times, now.Add(-lim.Window)); c > lim.Threshold {
			return lim, c, true
		}
	}
	return DDoSLimit{}, 0, false
}

func ipTrafficCounts(times []time.Time, now time.Time, limits []DDoSLimit) map[string]int {
	out := make(map[string]int, len(limits))
	for _, lim := range limits {
		out[lim.Name] = countSince(times, now.Add(-lim.Window))
	}
	return out
}

func (g *DDoSGuard) recordAndCheck(ip string) (DDoSLimit, int, bool) {
	now := time.Now()
	cutoff := now.Add(-g.maxRetention)

	g.mu.Lock()
	defer g.mu.Unlock()

	e := g.traffic[ip]
	if e == nil {
		e = &IPEntry{}
		g.traffic[ip] = e
	}
	e.times = append(pruneTimes(e.times, cutoff), now)
	return checkDDoSLimits(e.times, now, g.cfg.Limits)
}

func (g *DDoSGuard) autoBlock(ip string, lim DDoSLimit, count int64) {
	g.mu.Lock()
	if g.blocked[ip] {
		g.mu.Unlock()
		return
	}
	g.blocked[ip] = true
	g.mu.Unlock()

	notes := fmt.Sprintf("exceeded %s limit (%d/%d)", lim.Name, count, lim.Threshold)
	log.Printf("[ddos] auto-blocking %s (%s)", ip, notes)
	_ = g.db.AddBlockedIP(context.Background(), ip, "auto", notes, count)
}

func (g *DDoSGuard) BlockIP(ip, notes string) error {
	g.mu.Lock()
	g.blocked[ip] = true
	g.mu.Unlock()
	return g.db.AddBlockedIP(context.Background(), ip, "manual", notes, 0)
}

func (g *DDoSGuard) UnblockIP(ip string) error {
	g.mu.Lock()
	delete(g.blocked, ip)
	g.mu.Unlock()
	return g.db.RemoveBlockedIP(context.Background(), ip)
}

func (g *DDoSGuard) GetTopIPs(n int) []pkgmodels.IPTrafficStat {
	now := time.Now()

	g.mu.RLock()
	defer g.mu.RUnlock()

	type kv struct {
		ip     string
		counts map[string]int
	}
	pairs := make([]kv, 0, len(g.traffic))
	for ip, e := range g.traffic {
		counts := ipTrafficCounts(e.times, now, g.cfg.Limits)
		if counts["1m"] > 0 || counts["1h"] > 0 || counts["1mo"] > 0 {
			pairs = append(pairs, kv{ip: ip, counts: counts})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].counts["1m"] != pairs[j].counts["1m"] {
			return pairs[i].counts["1m"] > pairs[j].counts["1m"]
		}
		return pairs[i].counts["1h"] > pairs[j].counts["1h"]
	})
	if n > 0 && len(pairs) > n {
		pairs = pairs[:n]
	}

	stats := make([]pkgmodels.IPTrafficStat, len(pairs))
	for i, p := range pairs {
		c := p.counts
		stats[i] = pkgmodels.IPTrafficStat{
			IP:           p.ip,
			RequestCount: c["1m"],
			Count1h:      c["1h"],
			Count6h:      c["6h"],
			Count1d:      c["1d"],
			Count1w:      c["1w"],
			Count1mo:     c["1mo"],
			IsBlocked:    g.blocked[p.ip],
		}
	}
	return stats
}

func (g *DDoSGuard) IsBlocked(ip string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.blocked[ip]
}

func (g *DDoSGuard) BlockedIPs(ctx context.Context) ([]*pkgmodels.BlockedIP, error) {
	return g.db.GetBlockedIPs(ctx)
}
