package store

import (
	"sort"
	"sync"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
)

type Memory struct {
	mutex    sync.RWMutex
	capacity int
	items    map[string]capture.Exchange
}

func NewMemory(capacity int) *Memory {
	if capacity < 1 {
		capacity = 1
	}
	return &Memory{capacity: capacity, items: make(map[string]capture.Exchange)}
}

func (memory *Memory) Save(exchange capture.Exchange) []capture.Exchange {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()
	memory.items[exchange.ID] = exchange.Clone()
	return memory.evictExcess()
}

func (memory *Memory) Get(id string) (capture.Exchange, bool) {
	memory.mutex.RLock()
	defer memory.mutex.RUnlock()
	exchange, ok := memory.items[id]
	return exchange.Clone(), ok
}

func (memory *Memory) List() []capture.Exchange {
	memory.mutex.RLock()
	defer memory.mutex.RUnlock()
	result := make([]capture.Exchange, 0, len(memory.items))
	for _, exchange := range memory.items {
		result = append(result, exchange.Clone())
	}
	sortNewestFirst(result)
	return result
}

func (memory *Memory) Resize(capacity int) []capture.Exchange {
	if capacity < 1 {
		capacity = 1
	}
	memory.mutex.Lock()
	defer memory.mutex.Unlock()
	memory.capacity = capacity
	return memory.evictExcess()
}

func (memory *Memory) Clear() []capture.Exchange {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()
	removed := make([]capture.Exchange, 0)
	for id, exchange := range memory.items {
		if capture.IsTerminal(exchange.State) {
			removed = append(removed, exchange.Clone())
			delete(memory.items, id)
		}
	}
	sortNewestFirst(removed)
	return removed
}

func (memory *Memory) Remove(id string) (capture.Exchange, bool) {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()
	exchange, ok := memory.items[id]
	if !ok || !capture.IsTerminal(exchange.State) {
		return capture.Exchange{}, false
	}
	delete(memory.items, id)
	return exchange.Clone(), true
}

func (memory *Memory) RemoveCompletedBefore(cutoff time.Time) []capture.Exchange {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()
	removed := make([]capture.Exchange, 0)
	for id, exchange := range memory.items {
		if capture.IsTerminal(exchange.State) && !exchange.CompletedAt.IsZero() && exchange.CompletedAt.Before(cutoff) {
			removed = append(removed, exchange.Clone())
			delete(memory.items, id)
		}
	}
	sortNewestFirst(removed)
	return removed
}

func (memory *Memory) evictExcess() []capture.Exchange {
	completed := make([]capture.Exchange, 0, len(memory.items))
	for _, exchange := range memory.items {
		if capture.IsTerminal(exchange.State) {
			completed = append(completed, exchange)
		}
	}
	if len(completed) <= memory.capacity {
		return nil
	}
	sortNewestFirst(completed)
	evicted := make([]capture.Exchange, 0, len(completed)-memory.capacity)
	for _, exchange := range completed[memory.capacity:] {
		evicted = append(evicted, exchange.Clone())
		delete(memory.items, exchange.ID)
	}
	return evicted
}

func sortNewestFirst(exchanges []capture.Exchange) {
	sort.SliceStable(exchanges, func(left, right int) bool {
		leftTime := orderingTime(exchanges[left])
		rightTime := orderingTime(exchanges[right])
		if leftTime.Equal(rightTime) {
			return exchanges[left].ID > exchanges[right].ID
		}
		return leftTime.After(rightTime)
	})
}

func orderingTime(exchange capture.Exchange) time.Time {
	if !exchange.CompletedAt.IsZero() {
		return exchange.CompletedAt
	}
	return exchange.StartedAt
}
