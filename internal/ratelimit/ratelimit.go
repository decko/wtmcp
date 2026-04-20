// Package ratelimit provides per-key token bucket rate limiting for
// tool calls and HTTP proxy requests.
package ratelimit

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Registry manages rate limiters by key (plugin name, domain, or "global").
type Registry struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	defaults map[string]rate.Limit
	global   *rate.Limiter
}

// New creates a Registry from configuration strings.
// defaultRate applies to keys without a specific override.
// overrides maps specific keys to their rate strings.
// globalRate applies across all keys (empty string = no global limit).
func New(defaultRate string, overrides map[string]string, globalRate string) (*Registry, error) {
	defaults := make(map[string]rate.Limit)

	var defaultLimit rate.Limit
	if defaultRate != "" {
		var err error
		defaultLimit, err = ParseRate(defaultRate)
		if err != nil {
			return nil, fmt.Errorf("default rate %q: %w", defaultRate, err)
		}
	}

	for key, rateStr := range overrides {
		limit, err := ParseRate(rateStr)
		if err != nil {
			return nil, fmt.Errorf("rate for %q: %w", key, err)
		}
		defaults[key] = limit
	}

	r := &Registry{
		limiters: make(map[string]*rate.Limiter),
		defaults: defaults,
	}

	if globalRate != "" {
		gl, err := ParseRate(globalRate)
		if err != nil {
			return nil, fmt.Errorf("global rate %q: %w", globalRate, err)
		}
		r.global = rate.NewLimiter(gl, burstFor(gl))
	}

	if defaultLimit > 0 {
		r.defaults[""] = defaultLimit
	}

	return r, nil
}

// Allow checks whether a request for the given key is allowed.
// Returns the wait duration if rate limited, or zero if allowed.
// Per-key limits are checked before the global limit to avoid
// consuming global tokens for requests rejected by a tighter
// per-key limit.
func (r *Registry) Allow(key string) time.Duration {
	if r == nil {
		return 0
	}

	limiter := r.getLimiter(key)
	if limiter != nil {
		res := limiter.Reserve()
		if d := res.Delay(); d > 0 {
			res.Cancel()
			return d
		}
	}

	if r.global != nil {
		res := r.global.Reserve()
		if d := res.Delay(); d > 0 {
			res.Cancel()
			return d
		}
	}

	return 0
}

func (r *Registry) getLimiter(key string) *rate.Limiter {
	r.mu.RLock()
	l, ok := r.limiters[key]
	r.mu.RUnlock()
	if ok {
		return l
	}

	limit, hasOverride := r.defaults[key]
	if !hasOverride {
		limit = r.defaults[""]
		if limit == 0 {
			return nil
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if l, ok := r.limiters[key]; ok {
		return l
	}

	l = rate.NewLimiter(limit, burstFor(limit))
	r.limiters[key] = l
	return l
}

func burstFor(limit rate.Limit) int {
	return max(int(limit), 1)
}

// ParseRate parses a rate string like "60/m", "10/s", "3600/h".
func ParseRate(s string) (rate.Limit, error) {
	num, unit, ok := strings.Cut(s, "/")
	if !ok {
		return 0, fmt.Errorf("invalid rate format %q: expected <number>/<unit>", s)
	}

	n, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid rate number %q", num)
	}

	var divisor float64
	switch strings.TrimSpace(unit) {
	case "s":
		divisor = 1
	case "m":
		divisor = 60
	case "h":
		divisor = 3600
	default:
		return 0, fmt.Errorf("invalid rate unit %q: expected s, m, or h", unit)
	}

	return rate.Limit(n / divisor), nil
}
