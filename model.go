package trp

import (
	"fmt"
	"sync"
	"time"
)

type SliceLink[T any] struct {
	Head []T
	Tail []T
}

func (sl *SliceLink[T]) Len() int {
	return len(sl.Head) + len(sl.Tail)
}

func (sl *SliceLink[T]) SubFromStart(start int) *SliceLink[T] {
	if start < len(sl.Head) {
		return &SliceLink[T]{
			Head: sl.Head[start:],
			Tail: sl.Tail,
		}
	} else {
		return &SliceLink[T]{
			Head: sl.Tail[start-len(sl.Head):],
		}
	}
}

func (sl *SliceLink[T]) SubToEnd(end int) *SliceLink[T] {
	if end < len(sl.Head) {
		return &SliceLink[T]{
			Head: sl.Head[:end],
		}
	} else {
		return &SliceLink[T]{
			Head: sl.Head,
			Tail: sl.Tail[:end-len(sl.Head)],
		}
	}
}

func (sl *SliceLink[T]) Sub(start, end int) *SliceLink[T] {
	if start < len(sl.Head) {
		if end < len(sl.Head) {
			return &SliceLink[T]{
				Head: sl.Head[start:end],
			}
		} else {
			return &SliceLink[T]{
				Head: sl.Head[start:],
				Tail: sl.Tail[:end-len(sl.Head)],
			}
		}
	} else {
		return &SliceLink[T]{
			Head: sl.Tail[start-len(sl.Head) : end-len(sl.Head)],
		}
	}
}

func (sl *SliceLink[T]) Get(index int) T {
	if index < len(sl.Head) {
		return sl.Head[index]
	} else {
		return sl.Tail[index-len(sl.Head)]
	}
}

func (sl *SliceLink[T]) Data() []T {
	if sl.Tail == nil {
		return sl.Head
	}
	bytes := make([]T, len(sl.Head)+len(sl.Tail))
	copy(bytes[:len(sl.Head)], sl.Head)
	copy(bytes[len(sl.Head):], sl.Tail)
	return bytes
}

type Circle[T comparable] struct {
	current *Node[T]
	lock    sync.RWMutex
}

type Node[T any] struct {
	value T
	next  *Node[T]
}

func (ss *Circle[T]) Add(item T) {
	ss.lock.Lock()
	defer ss.lock.Unlock()
	if ss.current == nil {
		ss.current = &Node[T]{
			value: item,
		}
		ss.current.next = ss.current
	} else {
		p := ss.current
		newItem := &Node[T]{
			value: item,
			next:  p,
		}
		for p.next != ss.current {
			p = p.next
		}
		p.next = newItem
		ss.current = newItem
	}
}

func (ss *Circle[T]) Next() (ret T) {
	ss.lock.RLock()
	defer ss.lock.RUnlock()
	ret, ss.current = ss.current.value, ss.current.next
	return
}

func (ss *Circle[T]) Remove(v T) {
	ss.lock.RLock()
	defer ss.lock.RUnlock()
	p := ss.current
	if p == nil {
		return
	}
	for p.value != v && p.next != ss.current {
		p = p.next
	}
	if p.value == v {
		q := ss.current.next
		for q.next != p {
			q = q.next
		}
		q.next = p.next
		if ss.current == p {
			ss.current = p.next
		}
	}
}

func (ss *Circle[T]) Stringer() string {
	p := ss.current
	ts := make([]T, 0)
	ts = append(ts, p.value)
	for p.next != ss.current {
		ts = append(ts, p.next.value)
		p = p.next
	}
	return fmt.Sprintf("%+v", ts)
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
