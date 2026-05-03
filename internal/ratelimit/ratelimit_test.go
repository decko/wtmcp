package ratelimit

import (
	"fmt"
	"sync"
	"testing"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		wantRPS float64
	}{
		{"60/m", false, 1.0},
		{"1/s", false, 1.0},
		{"3600/h", false, 1.0},
		{"120/m", false, 2.0},
		{"10/s", false, 10.0},
		{"", true, 0},
		{"60", true, 0},
		{"abc/m", true, 0},
		{"60/x", true, 0},
		{"-1/s", true, 0},
		{"0/s", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			limit, err := ParseRate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := float64(limit)
			if got < tt.wantRPS-0.001 || got > tt.wantRPS+0.001 {
				t.Errorf("got %f, want %f", got, tt.wantRPS)
			}
		})
	}
}

func TestNewRegistry(t *testing.T) {
	r, err := New("60/m", map[string]string{"fast": "120/m"}, "300/m")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if d := r.Allow("normal"); d != 0 {
		t.Errorf("first request should be allowed, got delay %v", d)
	}

	if d := r.Allow("fast"); d != 0 {
		t.Errorf("first fast request should be allowed, got delay %v", d)
	}
}

func TestNewRegistryErrors(t *testing.T) {
	_, err := New("bad", nil, "")
	if err == nil {
		t.Error("expected error for bad default rate")
	}

	_, err = New("60/m", map[string]string{"k": "bad"}, "")
	if err == nil {
		t.Error("expected error for bad override rate")
	}

	_, err = New("60/m", nil, "bad")
	if err == nil {
		t.Error("expected error for bad global rate")
	}
}

func TestNilRegistryAllow(t *testing.T) {
	var r *Registry
	if d := r.Allow("anything"); d != 0 {
		t.Errorf("nil registry should allow, got delay %v", d)
	}
}

func TestNoDefaultRate(t *testing.T) {
	r, err := New("", map[string]string{"limited": "1/m"}, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if d := r.Allow("unlimited"); d != 0 {
		t.Error("key without rate should be allowed")
	}

	if d := r.Allow("limited"); d != 0 {
		t.Errorf("first limited request should be allowed, got delay %v", d)
	}
}

func TestExhaustedBurst(t *testing.T) {
	r, err := New("1/s", nil, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if d := r.Allow("key"); d != 0 {
		t.Errorf("first request should be allowed, got delay %v", d)
	}

	if d := r.Allow("key"); d == 0 {
		t.Error("second request should be rate-limited")
	}
}

func TestGlobalExhaustion(t *testing.T) {
	r, err := New("", nil, "1/s")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if d := r.Allow("a"); d != 0 {
		t.Errorf("first request should be allowed, got delay %v", d)
	}

	if d := r.Allow("b"); d == 0 {
		t.Error("second request (different key) should be globally rate-limited")
	}
}

func TestPerKeyBeforeGlobal(t *testing.T) {
	r, err := New("1/s", nil, "100/s")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if d := r.Allow("key"); d != 0 {
		t.Errorf("first request should be allowed, got delay %v", d)
	}

	if d := r.Allow("key"); d == 0 {
		t.Error("second request should be per-key rate-limited")
	}

	// Global should still have tokens since per-key rejected first
	if d := r.Allow("other"); d != 0 {
		t.Errorf("different key should be allowed (global not wasted), got delay %v", d)
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r, err := New("10/s", nil, "100/s")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("plugin-%d", i%5)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.Allow(key)
			}
		}()
	}
	wg.Wait()
}

func TestEvictionAtMaxEntries(t *testing.T) {
	r, err := New("10/s", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r.maxEntries = 5

	for i := range 10 {
		r.Allow(fmt.Sprintf("key-%d", i))
	}

	if r.Len() > 5 {
		t.Errorf("Len() = %d, want <= 5", r.Len())
	}
}

func TestEvictedKeyGetsFreshLimiter(t *testing.T) {
	r, err := New("1/s", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r.maxEntries = 2

	r.Allow("a")
	r.Allow("a") // exhaust burst for "a"

	// Fill with other keys to evict "a"
	r.Allow("b")
	r.Allow("c") // evicts "a"

	// "a" should get a fresh limiter with full burst
	if d := r.Allow("a"); d != 0 {
		t.Errorf("evicted key should get fresh limiter, got delay %v", d)
	}
}

func TestEvictionConcurrent(t *testing.T) {
	r, err := New("10/s", nil, "100/s")
	if err != nil {
		t.Fatal(err)
	}
	r.maxEntries = 10

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range 20 {
				r.Allow(fmt.Sprintf("key-%d-%d", n, j))
			}
		}(i)
	}
	wg.Wait()

	if r.Len() > 10 {
		t.Errorf("Len() = %d, want <= 10", r.Len())
	}
}

func TestBurstFor(t *testing.T) {
	if b := burstFor(0.5); b != 1 {
		t.Errorf("burstFor(0.5) = %d, want 1", b)
	}
	if b := burstFor(10); b != 10 {
		t.Errorf("burstFor(10) = %d, want 10", b)
	}
}
