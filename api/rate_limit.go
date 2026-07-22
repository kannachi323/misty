package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SlidingWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	history map[string][]time.Time
	// maxKeys bounds the tracked callers. Without it, a caller that varies its
	// key (a spoofed address, a sprayed path) grows this map until the process
	// is killed — the limiter becomes the denial of service.
	maxKeys int
}

type ForgotPasswordRateLimiter struct {
	*SlidingWindowLimiter
}

type RateLimitPolicy struct {
	Limit  int
	Window time.Duration
}

type APIRateLimiter struct {
	mu            sync.Mutex
	now           func() time.Time
	defaultGET    RateLimitPolicy
	defaultWrite  RateLimitPolicy
	routePolicies map[string]RateLimitPolicy
	limiters      map[string]*SlidingWindowLimiter
	// abuse escalates repeated rejections into a temporary block.
	abuse *AbuseGuard
}

// WithAbuseGuard wires rejections into the shared abuse guard.
func (l *APIRateLimiter) WithAbuseGuard(guard *AbuseGuard) *APIRateLimiter {
	l.abuse = guard
	return l
}

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = time.Minute
	}

	return &SlidingWindowLimiter{
		limit:   limit,
		window:  window,
		history: make(map[string][]time.Time),
		maxKeys: defaultLimiterMaxKeys,
	}
}

// defaultLimiterMaxKeys caps distinct callers tracked per limiter. Well past
// any realistic concurrent client count, small enough to bound memory.
const defaultLimiterMaxKeys = 20000

// Keys are client addresses, so this tracks distinct callers per route.

func NewForgotPasswordRateLimiter(limit int, window time.Duration) *ForgotPasswordRateLimiter {
	return &ForgotPasswordRateLimiter{
		SlidingWindowLimiter: NewSlidingWindowLimiter(limit, window),
	}
}

func NewAPIRateLimiter() *APIRateLimiter {
	return &APIRateLimiter{
		now:          time.Now,
		defaultGET:   RateLimitPolicy{Limit: 120, Window: time.Minute},
		defaultWrite: RateLimitPolicy{Limit: 30, Window: time.Minute},
		routePolicies: map[string]RateLimitPolicy{
			"POST /register":                                {Limit: 10, Window: time.Minute},
			"POST /login":                                   {Limit: 20, Window: time.Minute},
			"POST /waitlist":                                {Limit: 10, Window: time.Minute},
			"POST /auth/forgot":                             {Limit: 8, Window: time.Minute},
			"GET /auth/reset/start":                         {Limit: 20, Window: time.Minute},
			"GET /auth/reset/validate":                      {Limit: 20, Window: time.Minute},
			"POST /auth/reset":                              {Limit: 10, Window: time.Minute},
			"POST /billing/trial/start":                     {Limit: 10, Window: time.Minute},
			"POST /billing/checkout-session":                {Limit: 10, Window: time.Minute},
			"POST /billing/credit-checkout-session":         {Limit: 10, Window: time.Minute},
			"POST /billing/portal-session":                  {Limit: 20, Window: time.Minute},
			"POST /stripe/webhook":                          {Limit: 120, Window: time.Minute},
			"POST /ai/complete":                             {Limit: 12, Window: time.Minute},
			"POST /ai/sessions":                             {Limit: 20, Window: time.Hour},
			"POST /ai/sessions/{sessionID}/messages":        {Limit: 12, Window: time.Minute},
			"POST /ai/sessions/{sessionID}/tool-results":    {Limit: 30, Window: time.Minute},
			"GET /ai/sessions/{sessionID}/events":           {Limit: 120, Window: time.Minute},
			"POST /ai/sessions/{sessionID}/cancel":          {Limit: 30, Window: time.Minute},
			"POST /ai/media-search/chunks":                  {Limit: 60, Window: time.Minute},
			"POST /ai/media-search/search":                  {Limit: 60, Window: time.Minute},
			"POST /spaces/{spaceID}/library/reauthenticate": {Limit: 5, Window: time.Minute},

			// Provider fan-out: each of these makes an upstream call on Misty's own
			// credentials, so abuse burns third-party quota and can get the app
			// rate limited or banned rather than merely costing us CPU.
			"POST /spaces/{spaceID}/integrations/discord/link":              {Limit: 5, Window: time.Minute},
			"POST /spaces/{spaceID}/integrations/discord/link/{id}/sync":    {Limit: 10, Window: time.Minute},
			"POST /spaces/{spaceID}/integrations/discord/link/{id}/publish": {Limit: 20, Window: time.Minute},
			"GET /spaces/{spaceID}/integrations/notion/sources":             {Limit: 10, Window: time.Minute},
			"GET /spaces/{spaceID}/integrations/notion/search":              {Limit: 20, Window: time.Minute},
			"POST /spaces/{spaceID}/integrations/notion/pages":              {Limit: 20, Window: time.Minute},
			"POST /spaces/{spaceID}/calendar/sync":                          {Limit: 6, Window: time.Minute},
			"POST /spaces/{spaceID}/tasks/calendar":                         {Limit: 20, Window: time.Minute},

			// OAuth start is cheap for us but creates state rows and drives users
			// at a third-party consent screen.
			"POST /spaces/{spaceID}/integrations/{provider}/authorize": {Limit: 10, Window: time.Minute},
		},
		limiters: make(map[string]*SlidingWindowLimiter),
	}
}

func (l *SlidingWindowLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	requests := l.history[key][:0]
	for _, ts := range l.history[key] {
		if ts.After(cutoff) {
			requests = append(requests, ts)
		}
	}

	if len(requests) == 0 && len(l.history) >= l.maxKeys {
		// A new caller arriving at capacity: drop entries whose window has
		// fully elapsed, which is the common case under a spray.
		l.purgeExpired(cutoff)
	}
	if len(requests) == 0 && len(l.history) >= l.maxKeys {
		// Still full of live entries, so this is sustained abuse rather than
		// churn. Refuse rather than grow without bound.
		return false, l.window
	}

	if len(requests) >= l.limit {
		l.history[key] = requests
		retryAfter := requests[0].Add(l.window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter
	}

	l.history[key] = append(requests, now)
	return true, 0
}

// purgeExpired drops callers with no activity inside the current window.
// The caller must hold the mutex.
func (l *SlidingWindowLimiter) purgeExpired(cutoff time.Time) {
	for key, timestamps := range l.history {
		live := false
		for _, timestamp := range timestamps {
			if timestamp.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(l.history, key)
		}
	}
}

// TrackedKeys reports how many callers this limiter is holding state for.
func (l *SlidingWindowLimiter) TrackedKeys() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.history)
}

func (l *APIRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		path := normalizeRateLimitPath(r.URL.Path)
		policy := l.policyFor(r.Method, path)
		limiter := l.limiterFor(r.Method, path, policy)
		// Cost-bearing routes are charged to the account, so spreading a
		// credential across many addresses buys no extra budget. Everything
		// else stays keyed on the address, which is the only identity an
		// unauthenticated caller has.
		key := clientIPFromRequest(r)
		if costBearingRoutes[path] {
			key = rateLimitIdentity(r)
		}
		allowed, retryAfter := limiter.Allow(key, l.now())
		if !allowed {
			if l.abuse != nil {
				l.abuse.RecordRejection(key)
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(retryAfter)))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (l *APIRateLimiter) policyFor(method, path string) RateLimitPolicy {
	if policy, ok := l.routePolicies[method+" "+path]; ok {
		return policy
	}
	if method == http.MethodGet {
		return l.defaultGET
	}
	return l.defaultWrite
}

// maxTrackedRoutes bounds the limiters map. Real deployments register far
// fewer route shapes; anything beyond this is a caller inventing paths, and
// those share one overflow bucket instead of allocating unboundedly.
const maxTrackedRoutes = 512

func (l *APIRateLimiter) limiterFor(method, path string, policy RateLimitPolicy) *SlidingWindowLimiter {
	key := method + " " + path

	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.limiters[key]
	if ok {
		return limiter
	}
	if len(l.limiters) >= maxTrackedRoutes {
		key = method + " " + overflowRouteKey
		if overflow, exists := l.limiters[key]; exists {
			return overflow
		}
	}
	limiter = NewSlidingWindowLimiter(policy.Limit, policy.Window)
	l.limiters[key] = limiter
	return limiter
}

const overflowRouteKey = "{overflow}"

// costBearingRoutes spend money or third-party quota on Misty's credentials, so
// they are charged per account rather than per address. Keyed by the normalized
// path the limiter already computes.
var costBearingRoutes = map[string]bool{
	"/ai/complete":                                             true,
	"/ai/sessions":                                             true,
	"/ai/sessions/{sessionID}/messages":                        true,
	"/ai/sessions/{sessionID}/tool-results":                    true,
	"/ai/media-search/chunks":                                  true,
	"/ai/media-search/search":                                  true,
	"/spaces/{spaceID}/calendar/sync":                          true,
	"/spaces/{spaceID}/tasks/calendar":                         true,
	"/spaces/{spaceID}/integrations/discord/link":              true,
	"/spaces/{spaceID}/integrations/discord/link/{id}/sync":    true,
	"/spaces/{spaceID}/integrations/discord/link/{id}/publish": true,
	"/spaces/{spaceID}/integrations/notion/sources":            true,
	"/spaces/{spaceID}/integrations/notion/search":             true,
	"/spaces/{spaceID}/integrations/notion/pages":              true,
	"/spaces/{spaceID}/integrations/{provider}/authorize":      true,
	// Egress and storage operations bill per byte and per request.
	"/spaces/{spaceID}/library/exports/download":     true,
	"/spaces/{spaceID}/library/items/{id}/download":  true,
	"/spaces/{spaceID}/attachments/{id}/download":    true,
	"/spaces/{spaceID}/library/shared/{id}/download": true,
}

func normalizeRateLimitPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "/"
	}
	if strings.HasPrefix(trimmed, "/api/") {
		trimmed = trimmed[len("/api"):]
	}
	if trimmed == "/api" {
		return "/"
	}
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 4 && parts[0] == "spaces" && parts[2] == "library" && parts[3] == "reauthenticate" {
		return "/spaces/{spaceID}/library/reauthenticate"
	}
	if len(parts) == 4 && parts[0] == "ai" && parts[1] == "sessions" {
		switch parts[3] {
		case "messages", "events", "tool-results", "cancel":
			return "/ai/sessions/{sessionID}/" + parts[3]
		}
	}
	// Space-scoped routes carry ids in the path. Collapsing them keeps one
	// budget per route shape instead of handing out a fresh budget per Space —
	// and keeps the number of tracked route templates bounded.
	if parts[0] == "spaces" && len(parts) >= 2 {
		parts[1] = "{spaceID}"
		if len(parts) == 5 && parts[2] == "integrations" && parts[4] == "authorize" {
			parts[3] = "{provider}"
		}
		for index := 2; index < len(parts); index++ {
			if looksLikeRatePathIdentifier(parts[index]) {
				parts[index] = "{id}"
			}
		}
		return "/" + strings.Join(parts, "/")
	}
	return trimmed
}

// looksLikeRatePathIdentifier reports whether a segment is a generated id
// rather than a fixed route word. Misty ids are prefixed UUIDs ("msg_<uuid>")
// or bare UUID/hex, and provider ids are similar, so length plus a separator or
// pure hex is a reliable signal without a per-route table.
func looksLikeRatePathIdentifier(segment string) bool {
	if len(segment) < 16 {
		return false
	}
	if strings.ContainsAny(segment, "_-") {
		return true
	}
	for _, character := range segment {
		isHex := (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func retryAfterSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 1
	}
	seconds := int(duration / time.Second)
	if duration%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func forgotPasswordRateLimitKey(r *http.Request, email string) string {
	return clientIPFromRequest(r) + "|" + strings.ToLower(strings.TrimSpace(email))
}
