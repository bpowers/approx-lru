package simplelru

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"math/rand"
	"time"
)

func newRand() *rand.Rand {
	seedBytes := make([]byte, 8)
	if _, err := crand.Read(seedBytes); err != nil {
		panic(err)
	}
	seed := binary.LittleEndian.Uint64(seedBytes)

	return rand.New(rand.NewSource(int64(seed)))
}

// EvictCallback is used to get a callback when a cache entry is evicted
type EvictCallback[K comparable, V any] func(key K, value V)

// LRU implements a non-thread safe fixed size LRU cache
type LRU[K comparable, V any] struct {
	rng     rand.Rand
	size    int
	data    []entry[K, V]
	items   map[K]int
	onEvict EvictCallback[K, V]
}

const randomProbes = 8

// entry is used to hold a value in the evictList
type entry[K comparable, V any] struct {
	lastUsed int64
	key      K
	value    V
}

// NewLRU constructs an LRU of the given size
func NewLRU[K comparable, V any](size int, onEvict EvictCallback[K, V]) (*LRU[K, V], error) {
	if size <= 0 {
		return nil, errors.New("must provide a positive size")
	}
	c := &LRU[K, V]{
		rng:     *newRand(),
		size:    size,
		data:    make([]entry[K, V], 0, size),
		items:   make(map[K]int),
		onEvict: onEvict,
	}
	return c, nil
}

// Purge is used to completely clear the cache.
func (c *LRU[K, V]) Purge() {
	for k, i := range c.items {
		if c.onEvict != nil {
			c.onEvict(k, c.data[i].value)
		}
	}
	c.data = c.data[0:0]
	c.items = make(map[K]int)
}

//go:noinline
func (c *LRU[K, V]) shuffle() {
	c.rng.Shuffle(len(c.data), func(i, j int) {
		c.items[c.data[i].key] = j
		c.items[c.data[j].key] = i

		c.data[i], c.data[j] = c.data[j], c.data[i]
	})
}

// Add adds a value to the cache.  Returns true if an eviction occurred.
func (c *LRU[K, V]) Add(key K, value V) (evicted bool) {
	// Check for existing item
	if i, ok := c.items[key]; ok {
		entry := &c.data[i]
		entry.lastUsed = time.Now().UnixNano()
		entry.value = value
		return false
	}

	// Add new item
	ent := entry[K, V]{time.Now().UnixNano(), key, value}

	if len(c.data) < c.size {
		i := len(c.data)
		c.data = append(c.data, ent)
		c.items[key] = i
		// if we have filled up the cache for the first time, shuffle
		// the items to ensure they are randomly distributed in the array.
		// we need this to ensure our random probing is correct.
		if len(c.data) == c.size {
			c.shuffle()
		}
	} else {
		evicted = true
		i := c.removeOldest()
		c.data[i] = ent
		c.items[key] = i
	}

	return
}

// Get looks up a key's value from the cache.
func (c *LRU[K, V]) Get(key K) (value V, ok bool) {
	if i, ok := c.items[key]; ok {
		entry := &c.data[i]
		entry.lastUsed = time.Now().UnixNano()
		return entry.value, true
	}
	return
}

// Contains checks if a key is in the cache, without updating the recent-ness
// or deleting it for being stale.
func (c *LRU[K, V]) Contains(key K) (ok bool) {
	_, ok = c.items[key]
	return ok
}

// Peek returns the key value (or undefined if not found) without updating
// the "recently used"-ness of the key.
func (c *LRU[K, V]) Peek(key K) (value V, ok bool) {
	if i, ok := c.items[key]; ok {
		return c.data[i].value, true
	}
	return value, false
}

// Remove removes the provided key from the cache, returning if the
// key was contained.
func (c *LRU[K, V]) Remove(key K) (present bool) {
	if i, ok := c.items[key]; ok {
		c.removeElement(i, c.data[i])
		return true
	}
	return false
}

// Len returns the number of items in the cache.
func (c *LRU[K, V]) Len() int {
	return len(c.items)
}

// Resize changes the cache size.
func (c *LRU[K, V]) Resize(size int) (evicted int) {
	diff := c.Len() - size
	if diff < 0 {
		diff = 0
	}
	for i := 0; i < diff; i++ {
		c.removeOldest()
	}
	c.size = size
	return diff
}

// removeOldest removes the oldest item from the cache.
func (c *LRU[K, V]) removeOldest() (off int) {
	size := c.Len()
	if size <= 0 {
		return -1
	}
	base := c.rng.Intn(size)
	oldestOff := base
	oldest := c.data[base]
	for j := 1; j < randomProbes; j++ {
		off := (base + j) % size
		candidate := &c.data[off]
		if candidate.lastUsed < oldest.lastUsed {
			oldestOff = off
			oldest = *candidate
		}
	}

	// we could have found an empty slot
	if oldest.lastUsed != 0 {
		c.removeElement(oldestOff, oldest)
	}
	return oldestOff
}

// removeElement is used to remove a given list element from the cache
func (c *LRU[K, V]) removeElement(i int, ent entry[K, V]) {
	c.data[i] = entry[K, V]{}
	delete(c.items, ent.key)
	if c.onEvict != nil {
		c.onEvict(ent.key, ent.value)
	}
}
