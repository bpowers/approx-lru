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
type EvictCallback func(key interface{}, value interface{})

// LRU implements a non-thread safe fixed size LRU cache
type LRU struct {
	rng     rand.Rand
	size    int
	data    []entry
	items   map[interface{}]int
	onEvict EvictCallback
}

const randomProbes = 6

// entry is used to hold a value in the evictList
type entry struct {
	lastUsed int64
	key      interface{}
	value    interface{}
}

// NewLRU constructs an LRU of the given size
func NewLRU(size int, onEvict EvictCallback) (*LRU, error) {
	if size <= 0 {
		return nil, errors.New("must provide a positive size")
	}
	c := &LRU{
		rng:     *newRand(),
		size:    size,
		data:    make([]entry, 0, size),
		items:   make(map[interface{}]int),
		onEvict: onEvict,
	}
	return c, nil
}

// Purge is used to completely clear the cache.
func (c *LRU) Purge() {
	for k, i := range c.items {
		if c.onEvict != nil {
			c.onEvict(k, c.data[i].value)
		}
	}
	c.data = c.data[0:0]
	c.items = make(map[interface{}]int)
}

//go:noinline
func (c *LRU) shuffle() {
	c.rng.Shuffle(len(c.data), func(i, j int) {
		c.items[c.data[i].key] = j
		c.items[c.data[j].key] = i

		c.data[i], c.data[j] = c.data[j], c.data[i]
	})
}

// Add adds a value to the cache.  Returns true if an eviction occurred.
func (c *LRU) Add(key, value interface{}) (evicted bool) {
	// Check for existing item
	if i, ok := c.items[key]; ok {
		entry := &c.data[i]
		entry.lastUsed = time.Now().UnixNano()
		entry.value = value
		return false
	}

	// Add new item
	ent := entry{time.Now().UnixNano(), key, value}

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
func (c *LRU) Get(key interface{}) (value interface{}, ok bool) {
	if i, ok := c.items[key]; ok {
		entry := &c.data[i]
		entry.lastUsed = time.Now().UnixNano()
		return entry.value, true
	}
	return
}

// Contains checks if a key is in the cache, without updating the recent-ness
// or deleting it for being stale.
func (c *LRU) Contains(key interface{}) (ok bool) {
	_, ok = c.items[key]
	return ok
}

// Peek returns the key value (or undefined if not found) without updating
// the "recently used"-ness of the key.
func (c *LRU) Peek(key interface{}) (value interface{}, ok bool) {
	if i, ok := c.items[key]; ok {
		return c.data[i].value, true
	}
	return nil, false
}

// Remove removes the provided key from the cache, returning if the
// key was contained.
func (c *LRU) Remove(key interface{}) (present bool) {
	if i, ok := c.items[key]; ok {
		c.removeElement(i, c.data[i])
		return true
	}
	return false
}

// Len returns the number of items in the cache.
func (c *LRU) Len() int {
	return len(c.items)
}

// Resize changes the cache size.
func (c *LRU) Resize(size int) (evicted int) {
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
func (c *LRU) removeOldest() (off int) {
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
	if oldest.key != nil {
		c.removeElement(oldestOff, oldest)
	}
	return oldestOff
}

// removeElement is used to remove a given list element from the cache
func (c *LRU) removeElement(i int, ent entry) {
	c.data[i] = entry{}
	delete(c.items, ent.key)
	if c.onEvict != nil {
		c.onEvict(ent.key, ent.value)
	}
}
