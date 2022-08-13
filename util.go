package trp

import (
	"encoding/binary"
	"sync"
	"time"
)

func Int64ToBytes(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

func BytesToInt64(bytes []byte) int64 {
	return int64(binary.LittleEndian.Uint64(bytes[0:8]))
}

type TTLCache struct {
	TTL       time.Duration
	storage   map[string]time.Time
	lock      sync.RWMutex
	lastClean time.Time
}

func NewTTLCache(ttl time.Duration) *TTLCache {
	cache := TTLCache{
		TTL:       ttl,
		storage:   make(map[string]time.Time, 0),
		lock:      sync.RWMutex{},
		lastClean: time.Now(),
	}
	return &cache
}

func (t *TTLCache) Filter(id string) bool {
	t.lock.RLock()
	v, exist := t.storage[id]
	t.lock.RUnlock()
	shouldPass := !exist || v.Before(time.Now())
	if shouldPass {
		t.lock.Lock()
		t.storage[id] = time.Now().Add(t.TTL)
		if t.lastClean.Add(time.Minute).Before(time.Now()) {
			for k, vv := range t.storage {
				if vv.Before(time.Now()) {
					delete(t.storage, k)
				}
			}
			t.lastClean = time.Now()
		}
		t.lock.Unlock()
	}
	return shouldPass
}
