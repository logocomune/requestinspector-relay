package store

import (
	"fmt"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
)

func TestMemoryNewestFirstEvictionResizeAndClear(t *testing.T) {
	repository := NewMemory(2)
	for index := 1; index <= 3; index++ {
		exchange := completedExchange(index)
		evicted := repository.Save(exchange)
		if index == 3 && (len(evicted) != 1 || evicted[0].ID != "1") {
			t.Fatalf("evicted = %+v", evicted)
		}
	}
	if got := ids(repository.List()); fmt.Sprint(got) != "[3 2]" {
		t.Fatalf("newest first = %v", got)
	}
	if evicted := repository.Resize(1); len(evicted) != 1 || evicted[0].ID != "2" {
		t.Fatalf("resize evicted = %+v", evicted)
	}
	if removed := repository.Clear(); len(removed) != 1 || len(repository.List()) != 0 {
		t.Fatalf("clear removed=%d remaining=%d", len(removed), len(repository.List()))
	}
}

func TestMemoryKeepsActiveOutsideCompletedCapacity(t *testing.T) {
	repository := NewMemory(1)
	active := completedExchange(1)
	active.State = capture.StateReceiving
	repository.Save(active)
	repository.Save(completedExchange(2))
	repository.Save(completedExchange(3))
	if _, ok := repository.Get("1"); !ok {
		t.Fatal("active exchange evicted")
	}
	if len(repository.List()) != 2 {
		t.Fatalf("list length = %d, want active plus one completed", len(repository.List()))
	}
}

func TestMemoryRemovesOnlyTerminalExchangesBeforeCutoff(t *testing.T) {
	repository := NewMemory(10)
	old := completedExchange(1)
	boundary := completedExchange(2)
	active := completedExchange(0)
	active.State = capture.StateReceiving
	repository.Save(old)
	repository.Save(boundary)
	repository.Save(active)
	removed := repository.RemoveCompletedBefore(boundary.CompletedAt)
	if len(removed) != 1 || removed[0].ID != old.ID {
		t.Fatalf("removed=%v", ids(removed))
	}
	if _, found := repository.Get(boundary.ID); !found {
		t.Fatal("cutoff boundary was removed")
	}
	if _, found := repository.Get(active.ID); !found {
		t.Fatal("active exchange was removed")
	}
}

func TestMemoryRemoveDeletesOnlyTerminalExchange(t *testing.T) {
	tests := []struct {
		name    string
		state   capture.State
		removed bool
	}{
		{name: "completed", state: capture.StateCompleted, removed: true},
		{name: "failed", state: capture.StateFailed, removed: true},
		{name: "cancelled", state: capture.StateCancelled, removed: true},
		{name: "receiving", state: capture.StateReceiving},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := NewMemory(10)
			exchange := completedExchange(index)
			exchange.State = test.state
			repository.Save(exchange)
			removed, ok := repository.Remove(exchange.ID)
			if ok != test.removed {
				t.Fatalf("removed=%+v ok=%v", removed, ok)
			}
			_, found := repository.Get(exchange.ID)
			if found == test.removed {
				t.Fatalf("remaining=%v removed=%v", found, test.removed)
			}
		})
	}
	if _, ok := NewMemory(1).Remove("missing"); ok {
		t.Fatal("missing exchange removed")
	}
}

func TestMemoryCapacityProperty(t *testing.T) {
	property := func(rawCapacity uint8, actions []byte) bool {
		capacity := int(rawCapacity%20) + 1
		repository := NewMemory(capacity)
		nextID := 0
		for _, action := range actions {
			switch action % 3 {
			case 0:
				repository.Save(completedExchange(nextID))
				nextID++
			case 1:
				capacity = int(action%20) + 1
				repository.Resize(capacity)
			case 2:
				repository.Clear()
			}
			items := repository.List()
			if len(items) > capacity {
				return false
			}
			for index := 1; index < len(items); index++ {
				if items[index-1].CompletedAt.Before(items[index].CompletedAt) {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryNormalizesInvalidCapacity(t *testing.T) {
	repository := NewMemory(0)
	repository.Save(completedExchange(1))
	repository.Save(completedExchange(2))
	if len(repository.List()) != 1 {
		t.Fatalf("normalized capacity retained %d exchanges", len(repository.List()))
	}
	repository.Resize(0)
	if len(repository.List()) != 1 {
		t.Fatalf("normalized resize retained %d exchanges", len(repository.List()))
	}
}

func TestMemoryConcurrentAccess(t *testing.T) {
	repository := NewMemory(20)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for index := 0; index < 100; index++ {
				repository.Save(completedExchange(worker*100 + index))
				_, _ = repository.Get(fmt.Sprint(index))
				_ = repository.List()
			}
		}(worker)
	}
	waitGroup.Wait()
	if len(repository.List()) > 20 {
		t.Fatal("repository exceeded capacity")
	}
}

func completedExchange(index int) capture.Exchange {
	completed := time.Unix(int64(index), 0).UTC()
	return capture.Exchange{ID: fmt.Sprint(index), State: capture.StateCompleted, StartedAt: completed.Add(-time.Second), CompletedAt: completed}
}

func ids(exchanges []capture.Exchange) []string {
	result := make([]string, len(exchanges))
	for index := range exchanges {
		result[index] = exchanges[index].ID
	}
	return result
}
