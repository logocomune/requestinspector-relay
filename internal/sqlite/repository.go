package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	_ "modernc.org/sqlite"
)

const SchemaVersion = 2

var (
	ErrClosed          = errors.New("sqlite repository is closed")
	ErrQueueFull       = errors.New("sqlite writer queue is full")
	ErrMaintenanceBusy = errors.New("sqlite maintenance is already running")
	ErrDiskSpace       = errors.New("insufficient free disk space for full vacuum")
)

type Options struct {
	Path             string
	QueueCount       int
	QueueBytes       int64
	BatchSize        int
	FlushInterval    time.Duration
	BusyTimeout      time.Duration
	CleanupBatchSize int
	CompactPages     int
	RetentionDays    int
	Now              func() time.Time
	OnWrite          func(string, error)
	OnCleanup        func(time.Time, int64)
}

type Boundary struct {
	CompletedAt time.Time
	ID          string
}

type Status struct {
	Enabled            bool      `json:"enabled"`
	Path               string    `json:"path,omitempty"`
	DatabaseBytes      int64     `json:"database_bytes"`
	WALBytes           int64     `json:"wal_bytes"`
	PageSize           int64     `json:"page_size"`
	PageCount          int64     `json:"page_count"`
	FreePages          int64     `json:"free_pages"`
	ExchangeCount      int64     `json:"exchange_count"`
	OldestCompletedAt  time.Time `json:"oldest_completed_at,omitempty"`
	NewestCompletedAt  time.Time `json:"newest_completed_at,omitempty"`
	QueueCount         int       `json:"queue_count"`
	QueueCapacity      int       `json:"queue_capacity"`
	ReservedCount      int       `json:"reserved_count"`
	ReservedBytes      int64     `json:"reserved_bytes"`
	QueueByteCapacity  int64     `json:"queue_byte_capacity"`
	Writes             uint64    `json:"writes"`
	WriteFailures      uint64    `json:"write_failures"`
	BusyFailures       uint64    `json:"busy_failures"`
	LastWriteLatencyUS int64     `json:"last_write_latency_us"`
	Maintenance        string    `json:"maintenance,omitempty"`
	MaintenanceSince   time.Time `json:"maintenance_since,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
	RetentionDays      int       `json:"retention_days"`
}

type CleanupPreview struct {
	KeepDays      int       `json:"keep_days"`
	Cutoff        time.Time `json:"cutoff"`
	ExchangeCount int64     `json:"exchange_count"`
	DatabaseBytes int64     `json:"database_bytes"`
}

type CleanupResult struct {
	Cutoff  time.Time `json:"cutoff"`
	Deleted int64     `json:"deleted"`
}

type CompactResult struct {
	FreePagesBefore int64 `json:"free_pages_before"`
	FreePagesAfter  int64 `json:"free_pages_after"`
	PagesReclaimed  int64 `json:"pages_reclaimed"`
	BytesReclaimed  int64 `json:"bytes_reclaimed"`
}

type Reservation struct {
	repository *Repository
	bytes      int64
	done       atomic.Bool
}

type writeItem struct {
	exchange capture.Exchange
	bytes    int64
}

type controlRequest struct {
	context context.Context
	run     func(context.Context, *sql.DB) (any, error)
	result  chan controlResult
}

type controlResult struct {
	value any
	err   error
}

type Repository struct {
	database *sql.DB
	options  Options
	writes   chan writeItem
	controls chan controlRequest
	closing  chan struct{}
	stop     chan struct{}
	done     chan struct{}

	mutex            sync.Mutex
	accepting        bool
	reservedCount    int
	reservedBytes    int64
	maintenance      string
	maintenanceSince time.Time
	lastError        string
	closeError       error
	closeOnce        sync.Once
	closeComplete    chan struct{}

	writeCount    atomic.Uint64
	writeFailures atomic.Uint64
	busyFailures  atomic.Uint64
	lastLatencyUS atomic.Int64
	retentionDays atomic.Int64
}

func Open(options Options) (*Repository, error) {
	applyDefaults(&options)
	if options.Path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(options.Path), 0o700); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	newDatabase := databaseIsNew(options.Path)
	database, err := sql.Open("sqlite", databaseDSN(options))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(8)
	if err := configure(database, options.BusyTimeout, newDatabase); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := migrate(database); err != nil {
		_ = database.Close()
		return nil, err
	}
	repository := &Repository{
		database: database, options: options, writes: make(chan writeItem, options.QueueCount),
		controls: make(chan controlRequest), closing: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}), closeComplete: make(chan struct{}), accepting: true,
	}
	repository.retentionDays.Store(int64(options.RetentionDays))
	go repository.runWriter()
	go repository.runRetention()
	return repository, nil
}

func databaseDSN(options Options) string {
	path := filepath.ToSlash(options.Path)
	if runtime.GOOS == "windows" && len(path) >= 2 && path[1] == ':' {
		path = "/" + path
	}
	location := url.URL{Scheme: "file", Path: path}
	query := location.Query()
	query.Set("_busy_timeout", fmt.Sprint(options.BusyTimeout.Milliseconds()))
	query.Set("_foreign_keys", "1")
	query.Set("_synchronous", "NORMAL")
	location.RawQuery = query.Encode()
	return location.String()
}

func applyDefaults(options *Options) {
	if options.QueueCount <= 0 {
		options.QueueCount = 256
	}
	if options.QueueBytes <= 0 {
		options.QueueBytes = 512 << 20
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 50
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = 25 * time.Millisecond
	}
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if options.CleanupBatchSize <= 0 {
		options.CleanupBatchSize = 500
	}
	if options.CompactPages <= 0 {
		options.CompactPages = 256
	}
	if options.Now == nil {
		options.Now = time.Now
	}
}

func databaseIsNew(path string) bool {
	info, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist) || err == nil && info.Size() == 0
}

func configure(database *sql.DB, busyTimeout time.Duration, newDatabase bool) error {
	if newDatabase {
		if _, err := database.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			return fmt.Errorf("enable incremental auto-vacuum: %w", err)
		}
	}
	statements := []string{
		fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout.Milliseconds()),
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	return nil
}

func migrate(database *sql.DB) error {
	transaction, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin sqlite migration: %w", err)
	}
	defer transaction.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS exchanges (
			id TEXT PRIMARY KEY, completed_ns INTEGER NOT NULL, started_ns INTEGER NOT NULL,
			method TEXT NOT NULL, path TEXT NOT NULL, mode TEXT NOT NULL, status INTEGER,
			state TEXT NOT NULL, duration_ns INTEGER NOT NULL, request_bytes INTEGER NOT NULL,
			response_bytes INTEGER NOT NULL, metadata BLOB NOT NULL, request_body BLOB NOT NULL,
			response_body BLOB
		)`,
		`CREATE INDEX IF NOT EXISTS exchanges_completed_id ON exchanges(completed_ns DESC, id DESC)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS udp_datagrams (
			exchange_id TEXT PRIMARY KEY REFERENCES exchanges(id) ON DELETE CASCADE,
			payload BLOB NOT NULL
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return fmt.Errorf("apply sqlite migration: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migration: %w", err)
	}
	return nil
}

func (repository *Repository) Reserve(bytes int64) (*Reservation, bool) {
	if bytes < 0 {
		bytes = 0
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if !repository.accepting || repository.maintenance == "vacuum" || repository.reservedCount >= repository.options.QueueCount || repository.reservedBytes+bytes > repository.options.QueueBytes {
		return nil, false
	}
	repository.reservedCount++
	repository.reservedBytes += bytes
	return &Reservation{repository: repository, bytes: bytes}, true
}

func (reservation *Reservation) Commit(exchange capture.Exchange) error {
	if reservation == nil || !reservation.done.CompareAndSwap(false, true) {
		return nil
	}
	repository := reservation.repository
	select {
	case repository.writes <- writeItem{exchange: exchange.Clone(), bytes: reservation.bytes}:
		return nil
	default:
		repository.release(reservation.bytes)
		return ErrQueueFull
	}
}

func (reservation *Reservation) Cancel() {
	if reservation != nil && reservation.done.CompareAndSwap(false, true) {
		reservation.repository.release(reservation.bytes)
	}
}

func (repository *Repository) release(bytes int64) {
	repository.mutex.Lock()
	repository.reservedCount--
	repository.reservedBytes -= bytes
	if repository.reservedCount < 0 {
		repository.reservedCount = 0
	}
	if repository.reservedBytes < 0 {
		repository.reservedBytes = 0
	}
	repository.mutex.Unlock()
}

func (repository *Repository) runWriter() {
	defer close(repository.done)
	for {
		select {
		case first := <-repository.writes:
			repository.writeBatch(repository.collectBatch(first))
		case control := <-repository.controls:
			value, err := control.run(control.context, repository.database)
			control.result <- controlResult{value: value, err: err}
		case <-repository.stop:
			return
		}
	}
}

func (repository *Repository) collectBatch(first writeItem) []writeItem {
	batch := []writeItem{first}
	timer := time.NewTimer(repository.options.FlushInterval)
	defer timer.Stop()
	for len(batch) < repository.options.BatchSize {
		select {
		case item := <-repository.writes:
			batch = append(batch, item)
		case <-repository.closing:
			return batch
		case <-timer.C:
			return batch
		}
	}
	return batch
}

func (repository *Repository) writeBatch(batch []writeItem) {
	started := time.Now()
	transaction, err := repository.database.Begin()
	if err == nil {
		statement, prepareError := transaction.Prepare(`INSERT INTO exchanges
			(id, completed_ns, started_ns, method, path, mode, status, state, duration_ns, request_bytes, response_bytes, metadata, request_body, response_body)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET completed_ns=excluded.completed_ns, started_ns=excluded.started_ns, method=excluded.method,
			path=excluded.path, mode=excluded.mode, status=excluded.status, state=excluded.state, duration_ns=excluded.duration_ns,
			request_bytes=excluded.request_bytes, response_bytes=excluded.response_bytes, metadata=excluded.metadata,
			request_body=excluded.request_body, response_body=excluded.response_body`)
		if prepareError != nil {
			err = prepareError
		} else {
			udpStatement, udpPrepareError := transaction.Prepare(`INSERT INTO udp_datagrams (exchange_id, payload) VALUES (?, ?)
				ON CONFLICT(exchange_id) DO UPDATE SET payload=excluded.payload`)
			if udpPrepareError != nil {
				_ = statement.Close()
				err = udpPrepareError
			} else {
				deleteUDPStatement, deletePrepareError := transaction.Prepare(`DELETE FROM udp_datagrams WHERE exchange_id=?`)
				if deletePrepareError != nil {
					_ = udpStatement.Close()
					_ = statement.Close()
					err = deletePrepareError
				} else {
					for _, item := range batch {
						if err = persist(statement, udpStatement, deleteUDPStatement, item.exchange); err != nil {
							break
						}
					}
					_ = deleteUDPStatement.Close()
				}
				_ = udpStatement.Close()
			}
			_ = statement.Close()
		}
		if err == nil {
			err = transaction.Commit()
		} else {
			_ = transaction.Rollback()
		}
	}
	repository.lastLatencyUS.Store(time.Since(started).Microseconds())
	if err != nil {
		repository.writeFailures.Add(uint64(len(batch)))
		if isBusy(err) {
			repository.busyFailures.Add(uint64(len(batch)))
		}
		repository.recordError(err)
	} else {
		repository.writeCount.Add(uint64(len(batch)))
	}
	for _, item := range batch {
		repository.release(item.bytes)
		if repository.options.OnWrite != nil {
			repository.options.OnWrite(item.exchange.ID, err)
		}
	}
}

type persistedExchange struct {
	Exchange      capture.Exchange       `json:"exchange"`
	Configuration capture.ConfigSnapshot `json:"configuration"`
}

func persist(statement, udpStatement, deleteUDPStatement *sql.Stmt, exchange capture.Exchange) error {
	metadataExchange := exchange.Clone()
	requestBody := append([]byte(nil), metadataExchange.Request.Body...)
	var udpPayload []byte
	if metadataExchange.Transport == capture.TransportUDP && metadataExchange.Datagram != nil {
		requestBody = nil
		udpPayload = append([]byte(nil), metadataExchange.Datagram.Payload...)
		if udpPayload == nil {
			udpPayload = []byte{}
		}
		metadataExchange.Datagram.Payload = nil
	}
	if requestBody == nil {
		requestBody = []byte{}
	}
	metadataExchange.Request.Body = nil
	var responseBody []byte
	var status any
	if metadataExchange.Response != nil {
		status = metadataExchange.Response.Status
		responseBody = append([]byte(nil), metadataExchange.Response.Body...)
		metadataExchange.Response.Body = nil
	}
	metadata, err := json.Marshal(persistedExchange{Exchange: metadataExchange, Configuration: exchange.Configuration})
	if err != nil {
		return fmt.Errorf("encode exchange %q: %w", exchange.ID, err)
	}
	_, err = statement.Exec(exchange.ID, exchange.CompletedAt.UnixNano(), exchange.StartedAt.UnixNano(), exchange.Request.Method,
		exchange.Request.Path, exchange.Mode, status, string(exchange.State), int64(exchange.Duration), len(requestBody), len(responseBody),
		metadata, requestBody, nullableBody(exchange.Response != nil, responseBody))
	if err != nil {
		return fmt.Errorf("persist exchange %q: %w", exchange.ID, err)
	}
	if exchange.Transport == capture.TransportUDP && exchange.Datagram != nil {
		if _, err := udpStatement.Exec(exchange.ID, udpPayload); err != nil {
			return fmt.Errorf("persist UDP datagram %q: %w", exchange.ID, err)
		}
	} else if _, err := deleteUDPStatement.Exec(exchange.ID); err != nil {
		return fmt.Errorf("remove UDP datagram %q: %w", exchange.ID, err)
	}
	return nil
}

func nullableBody(present bool, body []byte) any {
	if !present {
		return nil
	}
	return body
}

func (repository *Repository) Get(ctx context.Context, id string) (capture.Exchange, bool, error) {
	row := repository.database.QueryRowContext(ctx, `SELECT metadata FROM exchanges WHERE id=?`, id)
	var metadata []byte
	if err := row.Scan(&metadata); errors.Is(err, sql.ErrNoRows) {
		return capture.Exchange{}, false, nil
	} else if err != nil {
		return capture.Exchange{}, false, fmt.Errorf("read exchange: %w", err)
	}
	exchange, err := decodeExchange(metadata)
	if err != nil {
		return capture.Exchange{}, false, err
	}
	return exchange, true, nil
}

func (repository *Repository) Body(ctx context.Context, id string, response bool) ([]byte, bool, error) {
	row := repository.database.QueryRowContext(ctx, `SELECT e.metadata, e.request_body, e.response_body, d.payload
		FROM exchanges e LEFT JOIN udp_datagrams d ON d.exchange_id=e.id WHERE e.id=?`, id)
	var metadata, requestBody, responseBody, udpPayload []byte
	if err := row.Scan(&metadata, &requestBody, &responseBody, &udpPayload); errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("read exchange body: %w", err)
	}
	exchange, err := decodeExchange(metadata)
	if err != nil {
		return nil, false, err
	}
	if exchange.Transport == capture.TransportUDP && !response {
		if exchange.Datagram == nil {
			return nil, false, fmt.Errorf("read UDP datagram %q: missing metadata", id)
		}
		return append([]byte(nil), udpPayload...), true, nil
	}
	body := requestBody
	if response {
		body = responseBody
	}
	return append([]byte(nil), body...), true, nil
}

func (repository *Repository) List(ctx context.Context, boundary *Boundary, limit int) ([]capture.Exchange, bool, error) {
	if limit < 1 {
		return nil, false, nil
	}
	query := `SELECT metadata FROM exchanges`
	arguments := []any{}
	if boundary != nil {
		query += ` WHERE completed_ns < ? OR (completed_ns = ? AND id < ?)`
		nanoseconds := boundary.CompletedAt.UnixNano()
		arguments = append(arguments, nanoseconds, nanoseconds, boundary.ID)
	}
	query += ` ORDER BY completed_ns DESC, id DESC LIMIT ?`
	arguments = append(arguments, limit+1)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, false, fmt.Errorf("list exchanges: %w", err)
	}
	defer rows.Close()
	items := make([]capture.Exchange, 0, limit+1)
	for rows.Next() {
		var metadata []byte
		if err := rows.Scan(&metadata); err != nil {
			return nil, false, fmt.Errorf("scan exchange summary: %w", err)
		}
		exchange, err := decodeExchange(metadata)
		if err != nil {
			return nil, false, err
		}
		items = append(items, exchange)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate exchange summaries: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func decodeExchange(metadata []byte) (capture.Exchange, error) {
	var stored persistedExchange
	if err := json.Unmarshal(metadata, &stored); err != nil {
		return capture.Exchange{}, fmt.Errorf("decode persisted exchange: %w", err)
	}
	stored.Exchange.Configuration = stored.Configuration
	stored.Exchange.Persistence = &capture.PersistenceStatus{State: "persisted"}
	return stored.Exchange, nil
}

func (repository *Repository) ContainsBoundary(ctx context.Context, boundary Boundary) (bool, error) {
	var found int
	err := repository.database.QueryRowContext(ctx, `SELECT 1 FROM exchanges WHERE id=? AND completed_ns=?`, boundary.ID, boundary.CompletedAt.UnixNano()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (repository *Repository) CleanupPreview(ctx context.Context, keepDays int) (CleanupPreview, error) {
	if keepDays < 0 {
		return CleanupPreview{}, errors.New("keep_days must not be negative")
	}
	cutoff := repository.options.Now().UTC().AddDate(0, 0, -keepDays)
	var count int64
	if err := repository.database.QueryRowContext(ctx, `SELECT count(*) FROM exchanges WHERE completed_ns < ?`, cutoff.UnixNano()).Scan(&count); err != nil {
		return CleanupPreview{}, fmt.Errorf("preview cleanup: %w", err)
	}
	status, err := repository.Status(ctx)
	if err != nil {
		return CleanupPreview{}, err
	}
	return CleanupPreview{KeepDays: keepDays, Cutoff: cutoff, ExchangeCount: count, DatabaseBytes: status.DatabaseBytes}, nil
}

func (repository *Repository) Cleanup(ctx context.Context, keepDays int) (CleanupResult, error) {
	preview, err := repository.CleanupPreview(ctx, keepDays)
	if err != nil {
		return CleanupResult{}, err
	}
	if err := repository.beginMaintenance("cleanup"); err != nil {
		return CleanupResult{}, err
	}
	defer repository.endMaintenance()
	var deleted int64
	for {
		value, err := repository.control(ctx, func(ctx context.Context, database *sql.DB) (any, error) {
			result, err := database.ExecContext(ctx, `DELETE FROM exchanges WHERE id IN (SELECT id FROM exchanges WHERE completed_ns < ? ORDER BY completed_ns, id LIMIT ?)`, preview.Cutoff.UnixNano(), repository.options.CleanupBatchSize)
			if err != nil {
				return nil, err
			}
			return result.RowsAffected()
		})
		if err != nil {
			return CleanupResult{}, fmt.Errorf("cleanup sqlite history: %w", err)
		}
		count := value.(int64)
		deleted += count
		if count < int64(repository.options.CleanupBatchSize) {
			break
		}
		select {
		case <-ctx.Done():
			return CleanupResult{}, ctx.Err()
		default:
		}
	}
	if repository.options.OnCleanup != nil {
		repository.options.OnCleanup(preview.Cutoff, deleted)
	}
	return CleanupResult{Cutoff: preview.Cutoff, Deleted: deleted}, nil
}

func (repository *Repository) Compact(ctx context.Context) (CompactResult, error) {
	if err := repository.beginMaintenance("compact"); err != nil {
		return CompactResult{}, err
	}
	defer repository.endMaintenance()
	value, err := repository.control(ctx, func(ctx context.Context, database *sql.DB) (any, error) {
		before, pageSize, err := pageMetrics(ctx, database)
		if err != nil {
			return nil, err
		}
		if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
			return nil, err
		}
		if _, err := database.ExecContext(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", repository.options.CompactPages)); err != nil {
			return nil, err
		}
		after, _, err := pageMetrics(ctx, database)
		if err != nil {
			return nil, err
		}
		reclaimed := max64(0, before-after)
		return CompactResult{FreePagesBefore: before, FreePagesAfter: after, PagesReclaimed: reclaimed, BytesReclaimed: reclaimed * pageSize}, nil
	})
	if err != nil {
		return CompactResult{}, fmt.Errorf("compact sqlite history: %w", err)
	}
	return value.(CompactResult), nil
}

func (repository *Repository) Vacuum(ctx context.Context) error {
	if err := repository.beginMaintenance("vacuum"); err != nil {
		return err
	}
	defer repository.endMaintenance()
	if err := repository.waitForReservations(ctx); err != nil {
		return err
	}
	status, err := repository.Status(ctx)
	if err != nil {
		return err
	}
	free, err := availableDiskBytes(filepath.Dir(repository.options.Path))
	if err != nil {
		return fmt.Errorf("check vacuum disk space: %w", err)
	}
	if free < uint64(status.DatabaseBytes*2) {
		return ErrDiskSpace
	}
	_, err = repository.control(ctx, func(ctx context.Context, database *sql.DB) (any, error) {
		if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return nil, err
		}
		if _, err := database.ExecContext(ctx, `VACUUM`); err != nil {
			return nil, err
		}
		var health string
		if err := database.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&health); err != nil {
			return nil, err
		}
		if health != "ok" {
			return nil, fmt.Errorf("sqlite health check returned %q", health)
		}
		return nil, nil
	})
	if err != nil {
		repository.recordError(err)
		return fmt.Errorf("vacuum sqlite history: %w", err)
	}
	return nil
}

func (repository *Repository) Status(ctx context.Context) (Status, error) {
	status := Status{Enabled: true, Path: repository.options.Path, QueueCount: len(repository.writes), QueueCapacity: cap(repository.writes), QueueByteCapacity: repository.options.QueueBytes, Writes: repository.writeCount.Load(), WriteFailures: repository.writeFailures.Load(), BusyFailures: repository.busyFailures.Load(), LastWriteLatencyUS: repository.lastLatencyUS.Load()}
	status.RetentionDays = int(repository.retentionDays.Load())
	repository.mutex.Lock()
	status.ReservedCount, status.ReservedBytes = repository.reservedCount, repository.reservedBytes
	status.Maintenance, status.MaintenanceSince, status.LastError = repository.maintenance, repository.maintenanceSince, repository.lastError
	repository.mutex.Unlock()
	if info, err := os.Stat(repository.options.Path); err == nil {
		status.DatabaseBytes = info.Size()
	}
	if info, err := os.Stat(repository.options.Path + "-wal"); err == nil {
		status.WALBytes = info.Size()
	}
	if err := repository.database.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&status.PageSize); err != nil {
		return Status{}, err
	}
	if err := repository.database.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&status.PageCount); err != nil {
		return Status{}, err
	}
	if err := repository.database.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&status.FreePages); err != nil {
		return Status{}, err
	}
	var oldest, newest sql.NullInt64
	if err := repository.database.QueryRowContext(ctx, `SELECT count(*), min(completed_ns), max(completed_ns) FROM exchanges`).Scan(&status.ExchangeCount, &oldest, &newest); err != nil {
		return Status{}, err
	}
	if oldest.Valid {
		status.OldestCompletedAt = time.Unix(0, oldest.Int64).UTC()
	}
	if newest.Valid {
		status.NewestCompletedAt = time.Unix(0, newest.Int64).UTC()
	}
	return status, nil
}

func (repository *Repository) control(ctx context.Context, run func(context.Context, *sql.DB) (any, error)) (any, error) {
	request := controlRequest{context: ctx, run: run, result: make(chan controlResult, 1)}
	select {
	case repository.controls <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-repository.stop:
		return nil, ErrClosed
	}
	select {
	case result := <-request.result:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (repository *Repository) beginMaintenance(name string) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.maintenance != "" {
		return ErrMaintenanceBusy
	}
	repository.maintenance = name
	repository.maintenanceSince = repository.options.Now().UTC()
	return nil
}

func (repository *Repository) endMaintenance() {
	repository.mutex.Lock()
	repository.maintenance = ""
	repository.maintenanceSince = time.Time{}
	repository.mutex.Unlock()
}

func (repository *Repository) waitForReservations(ctx context.Context) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		repository.mutex.Lock()
		count := repository.reservedCount
		repository.mutex.Unlock()
		if count == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (repository *Repository) WaitIdle(ctx context.Context) error {
	return repository.waitForReservations(ctx)
}

func (repository *Repository) Delete(ctx context.Context, id string) (bool, error) {
	value, err := repository.control(ctx, func(ctx context.Context, database *sql.DB) (any, error) {
		result, err := database.ExecContext(ctx, `DELETE FROM exchanges WHERE id = ?`, id)
		if err != nil {
			return false, fmt.Errorf("delete exchange %q: %w", id, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("count deleted exchange %q: %w", id, err)
		}
		return rows > 0, nil
	})
	if err != nil {
		return false, err
	}
	deleted, ok := value.(bool)
	return deleted && ok, nil
}

func (repository *Repository) Clear(ctx context.Context) (int64, error) {
	value, err := repository.control(ctx, func(ctx context.Context, database *sql.DB) (any, error) {
		result, err := database.ExecContext(ctx, `DELETE FROM exchanges`)
		if err != nil {
			return int64(0), fmt.Errorf("clear exchanges: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return int64(0), fmt.Errorf("count cleared exchanges: %w", err)
		}
		return rows, nil
	})
	if err != nil {
		return 0, err
	}
	rows, ok := value.(int64)
	if !ok {
		return 0, nil
	}
	return rows, nil
}

func (repository *Repository) runRetention() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if days := int(repository.retentionDays.Load()); days > 0 {
				_, _ = repository.Cleanup(context.Background(), days)
			}
		case <-repository.stop:
			return
		}
	}
}

func (repository *Repository) SetRetentionDays(days int) {
	if days < 0 {
		days = 0
	}
	repository.retentionDays.Store(int64(days))
}

func (repository *Repository) Close(ctx context.Context) error {
	repository.closeOnce.Do(func() { go repository.finishClose() })
	select {
	case <-repository.closeComplete:
	case <-ctx.Done():
		return ctx.Err()
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	return repository.closeError
}

func (repository *Repository) finishClose() {
	repository.mutex.Lock()
	repository.accepting = false
	repository.mutex.Unlock()
	close(repository.closing)
	_ = repository.waitForReservations(context.Background())
	close(repository.stop)
	<-repository.done
	err := repository.database.Close()
	if err != nil {
		err = fmt.Errorf("close sqlite database: %w", err)
	}
	repository.mutex.Lock()
	repository.closeError = err
	repository.mutex.Unlock()
	close(repository.closeComplete)
}

func (repository *Repository) recordError(err error) {
	repository.mutex.Lock()
	repository.lastError = err.Error()
	repository.mutex.Unlock()
}

func pageMetrics(ctx context.Context, database *sql.DB) (int64, int64, error) {
	var freePages, pageSize int64
	if err := database.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freePages); err != nil {
		return 0, 0, err
	}
	if err := database.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, 0, err
	}
	return freePages, pageSize, nil
}

func isBusy(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database is busy"))
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
