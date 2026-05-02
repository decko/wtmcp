package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryStoreConfig controls size limits and background cleanup.
type MemoryStoreConfig struct {
	MaxEntriesPerPlugin int
	MaxEntrySize        int64
	CleanupInterval     time.Duration
}

type entry struct {
	value      json.RawMessage
	expires    time.Time
	lastAccess atomic.Int64 // unix nanos
}

func (e *entry) expired() bool {
	return !e.expires.IsZero() && time.Now().After(e.expires)
}

// MemoryStore is an in-memory cache backend with optional size limits
// and LRU eviction.
type MemoryStore struct {
	mu        sync.RWMutex
	entries   map[string]*entry
	nsCounts  map[string]int
	maxPerNS  int
	maxSize   int64
	stopClean chan struct{}
	closeOnce sync.Once
}

// NewMemoryStore creates an in-memory cache store with no size limits.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries:  make(map[string]*entry),
		nsCounts: make(map[string]int),
	}
}

// NewMemoryStoreWithConfig creates an in-memory cache store with
// per-namespace entry limits, max entry size, and optional background
// cleanup of expired entries.
func NewMemoryStoreWithConfig(cfg MemoryStoreConfig) *MemoryStore {
	m := &MemoryStore{
		entries:  make(map[string]*entry),
		nsCounts: make(map[string]int),
		maxPerNS: cfg.MaxEntriesPerPlugin,
		maxSize:  cfg.MaxEntrySize,
	}
	if cfg.CleanupInterval > 0 {
		m.startCleanup(cfg.CleanupInterval)
	}
	return m
}

// Get retrieves a value from the cache.
func (m *MemoryStore) Get(_ context.Context, namespace, key string) (json.RawMessage, bool, error) {
	sk := storageKey(namespace, key)

	m.mu.RLock()
	e, ok := m.entries[sk]
	if !ok {
		m.mu.RUnlock()
		return nil, false, nil
	}
	if e.expired() {
		m.mu.RUnlock()
		m.mu.Lock()
		if e2, ok2 := m.entries[sk]; ok2 && e2.expired() {
			delete(m.entries, sk)
			m.nsCounts[namespace]--
		}
		m.mu.Unlock()
		return nil, false, nil
	}
	e.lastAccess.Store(time.Now().UnixNano())
	m.mu.RUnlock()
	return e.value, true, nil
}

// Set stores a value in the cache with an optional TTL.
// A TTL of 0 means no expiry. Returns an error if the value exceeds
// the configured max entry size.
func (m *MemoryStore) Set(_ context.Context, namespace, key string, value json.RawMessage, ttl time.Duration) error {
	if m.maxSize > 0 && int64(len(value)) > m.maxSize {
		return fmt.Errorf("value exceeds max entry size (%d > %d)", len(value), m.maxSize)
	}

	sk := storageKey(namespace, key)
	e := &entry{value: value}
	if ttl > 0 {
		e.expires = time.Now().Add(ttl)
	}
	e.lastAccess.Store(time.Now().UnixNano())

	m.mu.Lock()
	_, exists := m.entries[sk]
	m.entries[sk] = e
	if !exists {
		m.nsCounts[namespace]++
	}
	if m.maxPerNS > 0 && m.nsCounts[namespace] > m.maxPerNS {
		m.evictLRU(namespace)
	}
	m.mu.Unlock()

	return nil
}

// Del removes a value from the cache. Returns true if the key existed.
func (m *MemoryStore) Del(_ context.Context, namespace, key string) (bool, error) {
	sk := storageKey(namespace, key)

	m.mu.Lock()
	_, existed := m.entries[sk]
	if existed {
		delete(m.entries, sk)
		m.nsCounts[namespace]--
	}
	m.mu.Unlock()

	return existed, nil
}

// List returns keys matching a glob pattern within a namespace.
// Results are capped at 1000 keys.
func (m *MemoryStore) List(_ context.Context, namespace, pattern string) ([]string, error) {
	prefix := namespace + "\x00"
	fullPattern := prefix + pattern

	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for sk, e := range m.entries {
		if e.expired() {
			continue
		}
		if len(sk) <= len(prefix) {
			continue
		}
		if sk[:len(prefix)] != prefix {
			continue
		}
		userKey := sk[len(prefix):]
		matched, err := filepath.Match(fullPattern, sk)
		if err != nil {
			return nil, err
		}
		if matched {
			keys = append(keys, userKey)
			if len(keys) >= 1000 {
				break
			}
		}
	}

	return keys, nil
}

// Flush removes all entries in a namespace. Returns the count of removed entries.
func (m *MemoryStore) Flush(_ context.Context, namespace string) (int, error) {
	prefix := namespace + "\x00"

	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for sk := range m.entries {
		if len(sk) > len(prefix) && sk[:len(prefix)] == prefix {
			delete(m.entries, sk)
			count++
		}
	}
	delete(m.nsCounts, namespace)
	return count, nil
}

// Close stops the background cleanup goroutine (if running).
// Safe to call multiple times.
func (m *MemoryStore) Close() error {
	m.closeOnce.Do(func() {
		if m.stopClean != nil {
			close(m.stopClean)
		}
	})
	return nil
}

// evictLRU removes entries from a namespace to enforce the per-namespace
// limit. First sweeps expired entries; if still over limit, evicts the
// least-recently-accessed live entry. Must be called under write lock.
func (m *MemoryStore) evictLRU(namespace string) {
	prefix := namespace + "\x00"
	now := time.Now()

	for k, e := range m.entries {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if !e.expires.IsZero() && now.After(e.expires) {
			delete(m.entries, k)
			m.nsCounts[namespace]--
		}
	}

	if m.nsCounts[namespace] <= m.maxPerNS {
		return
	}

	var oldestKey string
	var oldestAccess int64 = math.MaxInt64
	for k, e := range m.entries {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if access := e.lastAccess.Load(); access < oldestAccess {
			oldestAccess = access
			oldestKey = k
		}
	}
	if oldestKey != "" {
		delete(m.entries, oldestKey)
		m.nsCounts[namespace]--
	}
}

func (m *MemoryStore) startCleanup(interval time.Duration) {
	m.stopClean = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanup()
			case <-m.stopClean:
				return
			}
		}
	}()
}

func (m *MemoryStore) cleanup() {
	m.mu.RLock()
	now := time.Now()
	var expired []string
	for k, e := range m.entries {
		if !e.expires.IsZero() && now.After(e.expires) {
			expired = append(expired, k)
		}
	}
	m.mu.RUnlock()

	if len(expired) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range expired {
		if e, ok := m.entries[k]; ok && e.expired() {
			ns, _, _ := strings.Cut(k, "\x00")
			delete(m.entries, k)
			m.nsCounts[ns]--
		}
	}
}
