package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
	"github.com/logocomune/requestinspector-relay/internal/store"
)

type contractRepository interface {
	save(capture.Exchange) error
	get(string) (capture.Exchange, bool, error)
	list() ([]capture.Exchange, error)
	body(string) ([]byte, bool, error)
	close() error
}

func TestRepositoryContract(t *testing.T) {
	factories := map[string]func(*testing.T) contractRepository{
		"memory": func(*testing.T) contractRepository { return &memoryContract{repository: store.NewMemory(10)} },
		"sqlite": func(t *testing.T) contractRepository {
			repository, err := requestsqlite.Open(requestsqlite.Options{Path: filepath.Join(t.TempDir(), "history.db")})
			if err != nil {
				t.Fatal(err)
			}
			return &sqliteContract{repository: repository}
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			repository := factory(t)
			defer repository.close()
			for index := 1; index <= 3; index++ {
				completed := time.Unix(int64(index), 0).UTC()
				exchange := capture.Exchange{ID: fmt.Sprint(index), State: capture.StateCompleted, StartedAt: completed.Add(-time.Second), CompletedAt: completed, Request: capture.Request{Method: "POST", Path: "/contract", Body: []byte{byte(index)}, BodyComplete: true, ObservedBodyBytes: 1}}
				if err := repository.save(exchange); err != nil {
					t.Fatal(err)
				}
			}
			items, err := repository.list()
			if err != nil || len(items) != 3 || items[0].ID != "3" || items[2].ID != "1" {
				t.Fatalf("ordered items=%v err=%v", contractIDs(items), err)
			}
			exchange, found, err := repository.get("2")
			if err != nil || !found || exchange.Request.Path != "/contract" {
				t.Fatalf("detail=%+v found=%v err=%v", exchange, found, err)
			}
			body, found, err := repository.body("2")
			if err != nil || !found || len(body) != 1 || body[0] != 2 {
				t.Fatalf("body=%v found=%v err=%v", body, found, err)
			}
			if _, found, err := repository.get("missing"); err != nil || found {
				t.Fatalf("missing found=%v err=%v", found, err)
			}
		})
	}
}

type memoryContract struct{ repository *store.Memory }

func (adapter *memoryContract) save(exchange capture.Exchange) error {
	adapter.repository.Save(exchange)
	return nil
}
func (adapter *memoryContract) get(id string) (capture.Exchange, bool, error) {
	exchange, found := adapter.repository.Get(id)
	return exchange, found, nil
}
func (adapter *memoryContract) list() ([]capture.Exchange, error) {
	return adapter.repository.List(), nil
}
func (adapter *memoryContract) body(id string) ([]byte, bool, error) {
	exchange, found := adapter.repository.Get(id)
	return exchange.Request.Body, found, nil
}
func (*memoryContract) close() error { return nil }

type sqliteContract struct{ repository *requestsqlite.Repository }

func (adapter *sqliteContract) save(exchange capture.Exchange) error {
	reservation, ok := adapter.repository.Reserve(int64(len(exchange.Request.Body)))
	if !ok {
		return requestsqlite.ErrQueueFull
	}
	if err := reservation.Commit(exchange); err != nil {
		return err
	}
	return adapter.repository.WaitIdle(context.Background())
}
func (adapter *sqliteContract) get(id string) (capture.Exchange, bool, error) {
	return adapter.repository.Get(context.Background(), id)
}
func (adapter *sqliteContract) list() ([]capture.Exchange, error) {
	items, _, err := adapter.repository.List(context.Background(), nil, 10)
	return items, err
}
func (adapter *sqliteContract) body(id string) ([]byte, bool, error) {
	return adapter.repository.Body(context.Background(), id, false)
}
func (adapter *sqliteContract) close() error { return adapter.repository.Close(context.Background()) }

func contractIDs(items []capture.Exchange) []string {
	result := make([]string, len(items))
	for index := range items {
		result[index] = items[index].ID
	}
	return result
}
