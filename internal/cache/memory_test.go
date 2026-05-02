package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryStoreGetSet(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	val := json.RawMessage(`{"key":"value"}`)
	if err := s.Set(ctx, "plugin1", "mykey", val, 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, hit, err := s.Get(ctx, "plugin1", "mykey")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if string(got) != string(val) {
		t.Errorf("got %s, want %s", got, val)
	}
}

func TestMemoryStoreNamespaceIsolation(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if err := s.Set(ctx, "plugin1", "key", json.RawMessage(`"one"`), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "plugin2", "key", json.RawMessage(`"two"`), 0); err != nil {
		t.Fatal(err)
	}

	v1, _, _ := s.Get(ctx, "plugin1", "key")
	v2, _, _ := s.Get(ctx, "plugin2", "key")

	if string(v1) != `"one"` {
		t.Errorf("plugin1 key = %s, want %q", v1, "one")
	}
	if string(v2) != `"two"` {
		t.Errorf("plugin2 key = %s, want %q", v2, "two")
	}
}

func TestMemoryStoreMiss(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	_, hit, err := s.Get(ctx, "ns", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Error("expected cache miss")
	}
}

func TestMemoryStoreTTL(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if err := s.Set(ctx, "ns", "expiring", json.RawMessage(`true`), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Should be a hit immediately
	_, hit, _ := s.Get(ctx, "ns", "expiring")
	if !hit {
		t.Error("expected hit before TTL")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	_, hit, _ = s.Get(ctx, "ns", "expiring")
	if hit {
		t.Error("expected miss after TTL")
	}
}

func TestMemoryStoreDel(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if err := s.Set(ctx, "ns", "del-me", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}

	existed, err := s.Del(ctx, "ns", "del-me")
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Error("expected existed=true")
	}

	existed, _ = s.Del(ctx, "ns", "del-me")
	if existed {
		t.Error("expected existed=false for second delete")
	}

	_, hit, _ := s.Get(ctx, "ns", "del-me")
	if hit {
		t.Error("expected miss after delete")
	}
}

func TestMemoryStoreFlush(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for i := range 5 {
		key := "key" + string(rune('0'+i))
		if err := s.Set(ctx, "ns", key, json.RawMessage(`1`), 0); err != nil {
			t.Fatal(err)
		}
	}
	// Different namespace — should not be flushed
	if err := s.Set(ctx, "other", "key", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}

	count, err := s.Flush(ctx, "ns")
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("flushed %d, want 5", count)
	}

	// Other namespace intact
	_, hit, _ := s.Get(ctx, "other", "key")
	if !hit {
		t.Error("other namespace should not be affected by flush")
	}
}

func TestMemoryStoreList(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for _, key := range []string{"fields:PROJECT", "fields:TEAM", "issue:FOO-1"} {
		if err := s.Set(ctx, "jira", key, json.RawMessage(`{}`), 0); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := s.List(ctx, "jira", "fields:*")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Errorf("got %d keys, want 2: %v", len(keys), keys)
	}
}

func TestValidateKey(t *testing.T) {
	valid := []string{"key", "fields:PROJECT", "issue.FOO-123", "a", "a-b_c.d:e"}
	for _, k := range valid {
		if err := ValidateKey(k); err != nil {
			t.Errorf("ValidateKey(%q) should pass: %v", k, err)
		}
	}

	invalid := []string{"", "../etc/passwd", "key with spaces", "key\x00null", "/absolute", string(make([]byte, 513))}
	for _, k := range invalid {
		if err := ValidateKey(k); err == nil {
			t.Errorf("ValidateKey(%q) should fail", k)
		}
	}
}

// --- Size limits and LRU eviction tests ---

func testStore(maxPerNS int, maxSize int64) *MemoryStore {
	return NewMemoryStoreWithConfig(MemoryStoreConfig{
		MaxEntriesPerPlugin: maxPerNS,
		MaxEntrySize:        maxSize,
	})
}

func TestMaxEntrySizeRejected(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 100)

	big := json.RawMessage(strings.Repeat("x", 101))
	err := s.Set(ctx, "ns", "big", big, 0)
	if err == nil {
		t.Error("expected error for oversized entry")
	}
	if !strings.Contains(err.Error(), "max entry size") {
		t.Errorf("expected size error, got: %v", err)
	}
}

func TestMaxEntrySizeBoundary(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 100)

	exact := json.RawMessage(strings.Repeat("x", 100))
	if err := s.Set(ctx, "ns", "exact", exact, 0); err != nil {
		t.Errorf("value at exactly max size should succeed: %v", err)
	}
}

func TestMaxEntriesEvictsLRU(t *testing.T) {
	ctx := context.Background()
	s := testStore(3, 0)

	for i, key := range []string{"a", "b", "c"} {
		if err := s.Set(ctx, "ns", key, json.RawMessage(`1`), 0); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(i+1) * time.Millisecond)
	}

	if err := s.Set(ctx, "ns", "d", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}

	_, hit, _ := s.Get(ctx, "ns", "a")
	if hit {
		t.Error("oldest entry 'a' should have been evicted")
	}
	_, hit, _ = s.Get(ctx, "ns", "d")
	if !hit {
		t.Error("new entry 'd' should exist")
	}
}

func TestLRUAccessOrder(t *testing.T) {
	ctx := context.Background()
	s := testStore(3, 0)

	for i, key := range []string{"a", "b", "c"} {
		if err := s.Set(ctx, "ns", key, json.RawMessage(`1`), 0); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(i+1) * time.Millisecond)
	}

	// Access "a" to update its lastAccess — now "b" is the oldest
	_, _, _ = s.Get(ctx, "ns", "a")
	time.Sleep(time.Millisecond)

	if err := s.Set(ctx, "ns", "d", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}

	_, hitA, _ := s.Get(ctx, "ns", "a")
	_, hitB, _ := s.Get(ctx, "ns", "b")
	if !hitA {
		t.Error("recently-accessed 'a' should NOT be evicted")
	}
	if hitB {
		t.Error("least-recently-accessed 'b' should be evicted")
	}
}

func TestOverwriteDoesNotDoubleCount(t *testing.T) {
	ctx := context.Background()
	s := testStore(3, 0)

	s.Set(ctx, "ns", "key", json.RawMessage(`1`), 0) //nolint:errcheck,gosec
	s.Set(ctx, "ns", "key", json.RawMessage(`2`), 0) //nolint:errcheck,gosec

	s.mu.RLock()
	count := s.nsCounts["ns"]
	s.mu.RUnlock()

	if count != 1 {
		t.Errorf("overwrite should not double-count, got nsCounts=%d", count)
	}
}

func TestNamespaceIsolationOnEviction(t *testing.T) {
	ctx := context.Background()
	s := testStore(2, 0)

	s.Set(ctx, "ns-a", "x", json.RawMessage(`1`), 0) //nolint:errcheck,gosec
	s.Set(ctx, "ns-a", "y", json.RawMessage(`1`), 0) //nolint:errcheck,gosec
	s.Set(ctx, "ns-b", "z", json.RawMessage(`1`), 0) //nolint:errcheck,gosec

	// Trigger eviction in ns-a
	s.Set(ctx, "ns-a", "w", json.RawMessage(`1`), 0) //nolint:errcheck,gosec

	_, hit, _ := s.Get(ctx, "ns-b", "z")
	if !hit {
		t.Error("ns-b entry should not be affected by ns-a eviction")
	}
}

func TestCleanupRemovesExpired(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 0)

	s.Set(ctx, "ns", "old", json.RawMessage(`1`), time.Millisecond) //nolint:errcheck,gosec
	time.Sleep(2 * time.Millisecond)

	s.cleanup()

	_, hit, _ := s.Get(ctx, "ns", "old")
	if hit {
		t.Error("expired entry should be removed by cleanup")
	}
}

func TestCleanupPreservesNonExpired(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 0)

	s.Set(ctx, "ns", "fresh", json.RawMessage(`1`), time.Hour) //nolint:errcheck,gosec

	s.cleanup()

	_, hit, _ := s.Get(ctx, "ns", "fresh")
	if !hit {
		t.Error("non-expired entry should survive cleanup")
	}
}

func TestCleanupNoDoubleDecrement(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 0)

	s.Set(ctx, "ns", "expiring", json.RawMessage(`1`), time.Millisecond) //nolint:errcheck,gosec
	time.Sleep(2 * time.Millisecond)

	// Lazy-delete via Get
	_, _, _ = s.Get(ctx, "ns", "expiring")
	// Now call cleanup — entry already gone
	s.cleanup()

	s.mu.RLock()
	count := s.nsCounts["ns"]
	s.mu.RUnlock()

	if count < 0 {
		t.Errorf("nsCounts should not go negative, got %d", count)
	}
}

func TestCloseDoubleClose(t *testing.T) { //nolint:revive // t required by test framework
	s := NewMemoryStoreWithConfig(MemoryStoreConfig{
		CleanupInterval: time.Hour,
	})
	s.Close() //nolint:errcheck,gosec
	s.Close() //nolint:errcheck,gosec
}

func TestZeroConfigUnlimited(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for i := range 100 {
		key := fmt.Sprintf("key-%03d", i)
		s.Set(ctx, "ns", key, json.RawMessage(`1`), 0) //nolint:errcheck,gosec
	}

	s.mu.RLock()
	count := len(s.entries)
	s.mu.RUnlock()

	if count != 100 {
		t.Errorf("unlimited store should hold all entries, got %d", count)
	}
}

func TestNamespaceCountAfterDelFlush(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 0)

	s.Set(ctx, "ns", "a", json.RawMessage(`1`), 0) //nolint:errcheck,gosec
	s.Set(ctx, "ns", "b", json.RawMessage(`1`), 0) //nolint:errcheck,gosec

	s.Del(ctx, "ns", "a") //nolint:errcheck,gosec

	s.mu.RLock()
	count := s.nsCounts["ns"]
	s.mu.RUnlock()
	if count != 1 {
		t.Errorf("after del, expected count 1, got %d", count)
	}

	s.Flush(ctx, "ns") //nolint:errcheck,gosec

	s.mu.RLock()
	count = s.nsCounts["ns"]
	s.mu.RUnlock()
	if count != 0 {
		t.Errorf("after flush, expected count 0, got %d", count)
	}
}

func TestExpireCleanupThenDel(t *testing.T) {
	ctx := context.Background()
	s := testStore(0, 0)

	s.Set(ctx, "ns", "temp", json.RawMessage(`1`), time.Millisecond) //nolint:errcheck,gosec
	time.Sleep(2 * time.Millisecond)

	s.cleanup()

	// Del on already-cleaned entry
	existed, _ := s.Del(ctx, "ns", "temp")
	if existed {
		t.Error("entry should already be gone after cleanup")
	}

	s.mu.RLock()
	count := s.nsCounts["ns"]
	s.mu.RUnlock()

	if count != 0 {
		t.Errorf("nsCounts should be 0 after cleanup+del, got %d", count)
	}
}

func TestConcurrencyStress(t *testing.T) { //nolint:revive // t required by test framework
	ctx := context.Background()
	s := testStore(50, 0)

	var wg sync.WaitGroup
	for g := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ns := "ns"
			for i := range 100 {
				key := strings.Repeat("k", 1) + string(rune('a'+id)) + string(rune('0'+i%10))
				s.Set(ctx, ns, key, json.RawMessage(`1`), 50*time.Millisecond) //nolint:errcheck,gosec
				s.Get(ctx, ns, key)                                            //nolint:errcheck,gosec
				if i%3 == 0 {
					s.Del(ctx, ns, key) //nolint:errcheck,gosec
				}
			}
		}(g)
	}
	wg.Wait()
}
