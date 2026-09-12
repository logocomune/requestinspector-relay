package store

import (
	"github.com/logocomune/requestinspector-relay/internal/capture"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
)

type Repository struct {
	Memory     *Memory
	Persistent *requestsqlite.Repository
}

func NewRepository(memory *Memory, persistent *requestsqlite.Repository) *Repository {
	return &Repository{Memory: memory, Persistent: persistent}
}

func (repository *Repository) Save(exchange capture.Exchange) []capture.Exchange {
	return repository.Memory.Save(exchange)
}

func (repository *Repository) Reserve(bytes int64) (capture.PersistenceReservation, bool) {
	if repository.Persistent == nil {
		return nil, true
	}
	reservation, ok := repository.Persistent.Reserve(bytes)
	if !ok {
		return nil, false
	}
	return reservation, true
}
