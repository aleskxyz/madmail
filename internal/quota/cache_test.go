package quota

import (
	"sync"
	"testing"

	"github.com/themadorg/madmail/framework/log"
)

func TestCacheBasicOps(t *testing.T) {
	c := New(log.Logger{Name: "test"})

	// Load with initial data
	usedMap := map[string]int64{
		"alice@example.com": 1024,
		"bob@example.com":   2048,
	}
	quotaMap := map[string]int64{
		"alice@example.com": 10240, // custom quota
	}
	c.Load(usedMap, quotaMap, 5000)

	// alice should have custom quota
	e, miss := c.Get("alice@example.com")
	if miss {
		t.Fatal("expected hit for alice")
	}
	if e.UsedBytes != 1024 {
		t.Errorf("alice used = %d, want 1024", e.UsedBytes)
	}
	if e.MaxBytes != 10240 {
		t.Errorf("alice max = %d, want 10240", e.MaxBytes)
	}
	if e.IsDefault {
		t.Error("alice should not be default")
	}

	// bob should have default quota
	e, miss = c.Get("bob@example.com")
	if miss {
		t.Fatal("expected hit for bob")
	}
	if e.UsedBytes != 2048 {
		t.Errorf("bob used = %d, want 2048", e.UsedBytes)
	}
	if e.MaxBytes != 5000 {
		t.Errorf("bob max = %d, want 5000", e.MaxBytes)
	}
	if !e.IsDefault {
		t.Error("bob should be default")
	}

	// unknown user = cache miss with default
	e, miss = c.Get("charlie@example.com")
	if !miss {
		t.Fatal("expected miss for charlie")
	}
	if e.MaxBytes != 5000 {
		t.Errorf("charlie default max = %d, want 5000", e.MaxBytes)
	}
}

func TestCacheSetAndReset(t *testing.T) {
	c := New(log.Logger{Name: "test"})
	c.Load(map[string]int64{"u@x": 100}, nil, 1000)

	// Set custom quota
	c.SetMax("u@x", 5000)
	e, _ := c.Get("u@x")
	if e.MaxBytes != 5000 || e.IsDefault {
		t.Errorf("after SetMax: max=%d isDefault=%v", e.MaxBytes, e.IsDefault)
	}

	// Reset to default
	c.ResetMax("u@x")
	e, _ = c.Get("u@x")
	if e.MaxBytes != 1000 || !e.IsDefault {
		t.Errorf("after ResetMax: max=%d isDefault=%v", e.MaxBytes, e.IsDefault)
	}
}

func TestCacheSetDefaultQuota(t *testing.T) {
	c := New(log.Logger{Name: "test"})
	c.Load(map[string]int64{
		"a@x": 100,
		"b@x": 200,
	}, map[string]int64{
		"a@x": 9999, // custom
	}, 1000)

	// Change default from 1000 to 2000
	c.SetDefaultQuota(2000)

	// a should keep custom
	e, _ := c.Get("a@x")
	if e.MaxBytes != 9999 {
		t.Errorf("a max = %d, want 9999", e.MaxBytes)
	}

	// b should pick up new default
	e, _ = c.Get("b@x")
	if e.MaxBytes != 2000 {
		t.Errorf("b max = %d, want 2000", e.MaxBytes)
	}

	// new unknown miss should also use the new default
	e, _ = c.Get("z@x")
	if e.MaxBytes != 2000 {
		t.Errorf("z max = %d, want 2000", e.MaxBytes)
	}
}

func TestCacheAddUsed(t *testing.T) {
	c := New(log.Logger{Name: "test"})
	c.Load(map[string]int64{"u@x": 100}, nil, 1000)

	newUsed := c.AddUsed("u@x", 50)
	if newUsed != 150 {
		t.Errorf("AddUsed = %d, want 150", newUsed)
	}

	// AddUsed should not go below zero
	c.AddUsed("u@x", -200)
	e, _ := c.Get("u@x")
	if e.UsedBytes != 0 {
		t.Errorf("used = %d, want 0 (clamped)", e.UsedBytes)
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := New(log.Logger{Name: "test"})
	c.Load(map[string]int64{"u@x": 100}, nil, 1000)

	c.Invalidate("u@x")
	_, miss := c.Get("u@x")
	if !miss {
		t.Error("expected miss after Invalidate")
	}
}

func TestCacheConcurrency(t *testing.T) {
	c := New(log.Logger{Name: "test"})
	c.Load(map[string]int64{"u@x": 0}, nil, 10000)

	var wg sync.WaitGroup
	// 100 concurrent readers + 10 concurrent writers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				c.Get("u@x")
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.AddUsed("u@x", 1)
			}
		}()
	}
	wg.Wait()

	e, _ := c.Get("u@x")
	if e.UsedBytes != 1000 {
		t.Errorf("after concurrent AddUsed: used=%d, want 1000", e.UsedBytes)
	}
}
