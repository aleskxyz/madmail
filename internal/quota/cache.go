// Package quota provides an in-memory quota cache with RWMutex protection
// for fast lookup of per-user storage usage and limits.
//
// The cache is populated at startup from the database and kept in sync
// when admin operations (CLI, API) modify quotas. This avoids hitting
// the database on every IMAP GETQUOTA / delivery quota check.
package quota

import (
	"sync"

	"github.com/themadorg/madmail/framework/log"
)

// Entry holds the cached quota state for a single user.
type Entry struct {
	UsedBytes int64 // Current storage usage in bytes
	MaxBytes  int64 // Maximum allowed storage in bytes (0 = unlimited)
	IsDefault bool  // true if MaxBytes comes from the global default
}

// Cache is a concurrency-safe in-memory cache of per-user quota data.
// It uses a sync.RWMutex to allow many concurrent readers (IMAP quota
// checks, delivery checks) while still supporting exclusive writes
// (admin changes, periodic refresh).
//
// Deadlock prevention rules:
//   - Never call exported Cache methods while holding the lock yourself.
//   - Never hold mu while calling into the database or other subsystems.
//   - All lock scopes are contained within a single method — no nested locking.
type Cache struct {
	mu           sync.RWMutex
	entries      map[string]*Entry
	defaultQuota int64 // global default quota (bytes)
	log          log.Logger
}

// New creates an empty Cache ready for use.
func New(logger log.Logger) *Cache {
	return &Cache{
		entries: make(map[string]*Entry),
		log:     logger,
	}
}

// Load bulk-loads quota data into the cache. This is called once at startup.
// usedMap is username → used bytes (from SUM of message body lengths).
// quotaMap is username → max bytes (from the quotas table, 0 means no custom quota).
// defaultQuota is the global default limit in bytes.
func (c *Cache) Load(usedMap map[string]int64, quotaMap map[string]int64, defaultQuota int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.defaultQuota = defaultQuota
	c.entries = make(map[string]*Entry, len(usedMap))

	// Seed entries from per-user storage usage
	for username, used := range usedMap {
		c.entries[username] = &Entry{
			UsedBytes: used,
		}
	}

	// Apply per-user max quotas
	for username, maxBytes := range quotaMap {
		e, ok := c.entries[username]
		if !ok {
			e = &Entry{}
			c.entries[username] = e
		}
		if maxBytes > 0 {
			e.MaxBytes = maxBytes
			e.IsDefault = false
		}
	}

	// Fill in defaults for any entry that doesn't have a custom quota
	for _, e := range c.entries {
		if e.MaxBytes == 0 {
			e.MaxBytes = defaultQuota
			e.IsDefault = true
		}
	}

	c.log.Debugf("quota cache loaded: %d users, default=%d bytes", len(c.entries), defaultQuota)
}

// Get returns the cached quota for a user.
// If the user isn't in the cache, it returns the global default with zero usage
// and miss=true so the caller can optionally do a DB lookup.
func (c *Cache) Get(username string) (entry Entry, miss bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[username]
	if !ok {
		return Entry{
			UsedBytes: 0,
			MaxBytes:  c.defaultQuota,
			IsDefault: true,
		}, true
	}
	return *e, false
}

// SetMax updates the max quota for a user (admin set operation).
// This does NOT touch the database — the caller must persist the change first
// and then call this to keep the cache in sync.
func (c *Cache) SetMax(username string, maxBytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[username]
	if !ok {
		e = &Entry{}
		c.entries[username] = e
	}
	e.MaxBytes = maxBytes
	e.IsDefault = false
}

// ResetMax removes the per-user override and falls back to the global default.
func (c *Cache) ResetMax(username string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[username]
	if !ok {
		return
	}
	e.MaxBytes = c.defaultQuota
	e.IsDefault = true
}

// SetDefaultQuota updates the global default and recalculates all entries
// that were using the previous default.
func (c *Cache) SetDefaultQuota(newDefault int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldDefault := c.defaultQuota
	c.defaultQuota = newDefault

	for _, e := range c.entries {
		if e.IsDefault && e.MaxBytes == oldDefault {
			e.MaxBytes = newDefault
		}
	}
}

// UpdateUsed sets the used bytes for a user. Call this after a successful
// message delivery or deletion to keep the cache accurate.
func (c *Cache) UpdateUsed(username string, usedBytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[username]
	if !ok {
		e = &Entry{
			MaxBytes:  c.defaultQuota,
			IsDefault: true,
		}
		c.entries[username] = e
	}
	e.UsedBytes = usedBytes
}

// AddUsed atomically adds delta bytes to a user's used count.
// Returns the new used value.
func (c *Cache) AddUsed(username string, delta int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[username]
	if !ok {
		e = &Entry{
			MaxBytes:  c.defaultQuota,
			IsDefault: true,
		}
		c.entries[username] = e
	}
	e.UsedBytes += delta
	if e.UsedBytes < 0 {
		e.UsedBytes = 0
	}
	return e.UsedBytes
}

// Invalidate removes a user from the cache entirely.
// The next Get will return a cache miss.
func (c *Cache) Invalidate(username string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, username)
}

// InvalidateAll clears the entire cache.
func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*Entry)
}

// GetDefaultQuota returns the current global default quota.
func (c *Cache) GetDefaultQuota() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defaultQuota
}

// Size returns the number of entries in the cache (for diagnostics).
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
