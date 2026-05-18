package ratelimit

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

const cleanupLimit = 16

type shardedState[T any] struct {
	shards          []stateShard[T]
	idleTTL         time.Duration
	maxKeys         int
	stateFullPolicy StateFullPolicy
	count           atomic.Int64
}

type stateShard[T any] struct {
	mu    sync.Mutex
	items map[string]*stateEntry[T]
}

type stateEntry[T any] struct {
	value    *T
	lastSeen time.Time
}

func newShardedState[T any](config Config, window time.Duration) *shardedState[T] {
	shardCount := normalizeShards(config.Shards)
	state := &shardedState[T]{
		shards:          make([]stateShard[T], shardCount),
		idleTTL:         normalizeIdleTTL(config.IdleTTL, window),
		maxKeys:         config.MaxKeys,
		stateFullPolicy: config.StateFullPolicy,
	}
	for i := range state.shards {
		state.shards[i].items = make(map[string]*stateEntry[T])
	}
	return state
}

func (s *shardedState[T]) WithEntry(key string, now time.Time, create func(time.Time) T, use func(*T, bool) (Decision, error)) (Decision, error) {
	shard := s.shardFor(key)

	shard.mu.Lock()
	shard.cleanupLocked(now, s.idleTTL, &s.count)
	entry, ok := shard.items[key]
	if ok {
		entry.lastSeen = now
		decision, err := use(entry.value, false)
		shard.mu.Unlock()
		return decision, err
	}
	shard.mu.Unlock()

	if s.maxKeys > 0 && int(s.count.Load()) >= s.maxKeys {
		s.cleanupExpired(now)
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.cleanupLocked(now, s.idleTTL, &s.count)
	entry, ok = shard.items[key]
	if ok {
		entry.lastSeen = now
		return use(entry.value, false)
	}

	if s.maxKeys > 0 && int(s.count.Load()) >= s.maxKeys {
		if s.stateFullPolicy == AllowNewKey {
			value := create(now)
			return use(&value, true)
		}
		return Decision{Allowed: false}, ErrLimitStateFull
	}

	value := create(now)
	entry = &stateEntry[T]{
		value:    &value,
		lastSeen: now,
	}
	shard.items[key] = entry
	s.count.Add(1)
	return use(entry.value, true)
}

func (s *shardedState[T]) Reset(key string) bool {
	shard := s.shardFor(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.items[key]; !ok {
		return false
	}

	delete(shard.items, key)
	s.count.Add(-1)
	return true
}

func (s *shardedState[T]) ActiveKeys() int {
	return int(s.count.Load())
}

func (s *shardedState[T]) cleanupExpired(now time.Time) {
	for i := range s.shards {
		shard := &s.shards[i]
		shard.mu.Lock()
		shard.cleanupLocked(now, s.idleTTL, &s.count)
		shard.mu.Unlock()
	}
}

func (s *shardedState[T]) shardFor(key string) *stateShard[T] {
	idx := hashKey(key) % uint32(len(s.shards))
	return &s.shards[idx]
}

func (s *stateShard[T]) cleanupLocked(now time.Time, idleTTL time.Duration, count *atomic.Int64) {
	if idleTTL <= 0 {
		return
	}

	cleaned := 0
	for key, entry := range s.items {
		if cleaned >= cleanupLimit {
			return
		}
		if now.Sub(entry.lastSeen) <= idleTTL {
			continue
		}
		delete(s.items, key)
		count.Add(-1)
		cleaned++
	}
}

func hashKey(key string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return hash.Sum32()
}
