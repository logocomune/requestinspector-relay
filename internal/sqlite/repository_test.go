package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

func TestRepositoryConfiguresSchemaWALAndIncrementalVacuum(t *testing.T) {
	repository := openTestRepository(t, Options{})
	var journalMode string
	if err := repository.database.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	var autoVacuum, version int
	if err := repository.database.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		t.Fatal(err)
	}
	if err := repository.database.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" || autoVacuum != 2 || version != SchemaVersion {
		t.Fatalf("journal=%q auto_vacuum=%d schema=%d", journalMode, autoVacuum, version)
	}
	connection, err := repository.database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var busyTimeout, foreignKeys int
	if err := connection.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 || foreignKeys != 1 {
		t.Fatalf("connection busy_timeout=%d foreign_keys=%d", busyTimeout, foreignKeys)
	}
}

func TestDatabaseDSNEscapesPathAndPinsConnectionPragmas(t *testing.T) {
	dsn := databaseDSN(Options{Path: "/tmp/request?lens.db", BusyTimeout: 3 * time.Second})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/tmp/request?lens.db" || parsed.Query().Get("_busy_timeout") != "3000" || parsed.Query().Get("_foreign_keys") != "1" || parsed.Query().Get("_synchronous") != "NORMAL" {
		t.Fatalf("dsn=%q parsed=%+v", dsn, parsed)
	}
}

func TestOpenRejectsMissingPathAndUnusableDirectory(t *testing.T) {
	if _, err := Open(Options{}); err == nil {
		t.Fatal("empty path accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Path: filepath.Join(file, "history.db")}); err == nil {
		t.Fatal("file accepted as database directory")
	}
}

func TestRepositoryRoundTripOrderingPaginationAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	repository := openTestRepository(t, Options{Path: path})
	for index := 1; index <= 3; index++ {
		persistTestExchange(t, repository, testExchange(fmt.Sprint(index), time.Unix(int64(index), 0), []byte{byte(index), 0xff}))
	}
	page, more, err := repository.List(context.Background(), nil, 2)
	if err != nil || !more || ids(page) != "3,2" {
		t.Fatalf("page=%s more=%v err=%v", ids(page), more, err)
	}
	boundary := &Boundary{CompletedAt: page[1].CompletedAt, ID: page[1].ID}
	page, more, err = repository.List(context.Background(), boundary, 2)
	if err != nil || more || ids(page) != "1" {
		t.Fatalf("older page=%s more=%v err=%v", ids(page), more, err)
	}
	stored, found, err := repository.Get(context.Background(), "3")
	if err != nil || !found || len(stored.Request.Body) != 0 || stored.Request.Method != http.MethodPost || stored.Configuration.Mode != config.ModeProxy {
		t.Fatalf("lazy detail=%+v found=%v err=%v", stored, found, err)
	}
	requestBody, found, err := repository.Body(context.Background(), "3", false)
	if err != nil || !found || !bytes.Equal(requestBody, []byte{3, 0xff}) {
		t.Fatalf("request body=%v found=%v err=%v", requestBody, found, err)
	}
	responseBody, found, err := repository.Body(context.Background(), "3", true)
	if err != nil || !found || string(responseBody) != "response" {
		t.Fatalf("response body=%q found=%v err=%v", responseBody, found, err)
	}
	if err := repository.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close(context.Background()) })
	page, _, err = restarted.List(context.Background(), nil, 10)
	if err != nil || ids(page) != "3,2,1" {
		t.Fatalf("restart page=%s err=%v", ids(page), err)
	}
}

func TestRepositoryMigratesLegacyHTTPAndSeparatesUDPPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	legacy := testExchange("legacy-http", time.Unix(1, 0), []byte("legacy-body"))
	createLegacyDatabase(t, path, legacy)

	repository := openTestRepository(t, Options{Path: path})
	udp := testUDPExchange("udp", time.Unix(2, 0), []byte{0x00, 0xff, 'u', 'd', 'p'})
	persistTestExchange(t, repository, udp)

	items, more, err := repository.List(context.Background(), nil, 10)
	if err != nil || more || ids(items) != "udp,legacy-http" {
		t.Fatalf("mixed page=%s more=%v err=%v", ids(items), more, err)
	}
	legacyBody, found, err := repository.Body(context.Background(), legacy.ID, false)
	if err != nil || !found || !bytes.Equal(legacyBody, legacy.Request.Body) {
		t.Fatalf("legacy body=%q found=%v err=%v", legacyBody, found, err)
	}
	stored, found, err := repository.Get(context.Background(), udp.ID)
	if err != nil || !found || stored.Datagram == nil || len(stored.Datagram.Payload) != 0 || stored.Datagram.SourceAddress != udp.Datagram.SourceAddress {
		t.Fatalf("lazy UDP detail=%+v found=%v err=%v", stored, found, err)
	}
	payload, found, err := repository.Body(context.Background(), udp.ID, false)
	if err != nil || !found || !bytes.Equal(payload, udp.Datagram.Payload) {
		t.Fatalf("UDP payload=%v found=%v err=%v", payload, found, err)
	}
	var requestBody, datagramPayload []byte
	if err := repository.database.QueryRow(`SELECT request_body FROM exchanges WHERE id=?`, udp.ID).Scan(&requestBody); err != nil {
		t.Fatal(err)
	}
	if err := repository.database.QueryRow(`SELECT payload FROM udp_datagrams WHERE exchange_id=?`, udp.ID).Scan(&datagramPayload); err != nil {
		t.Fatal(err)
	}
	if len(requestBody) != 0 || !bytes.Equal(datagramPayload, udp.Datagram.Payload) {
		t.Fatalf("HTTP request body=%v UDP detail payload=%v", requestBody, datagramPayload)
	}
	if err := repository.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close(context.Background()) })
	payload, found, err = restarted.Body(context.Background(), udp.ID, false)
	if err != nil || !found || !bytes.Equal(payload, udp.Datagram.Payload) {
		t.Fatalf("restart UDP payload=%v found=%v err=%v", payload, found, err)
	}
}

func TestReservationBoundsAndVacuumAdmissionExclusion(t *testing.T) {
	repository := openTestRepository(t, Options{QueueCount: 1, QueueBytes: 10})
	first, ok := repository.Reserve(10)
	if !ok {
		t.Fatal("first reservation rejected")
	}
	if _, ok := repository.Reserve(1); ok {
		t.Fatal("count and byte bounds accepted excess reservation")
	}
	first.Cancel()
	if err := repository.beginMaintenance("vacuum"); err != nil {
		t.Fatal(err)
	}
	if _, ok := repository.Reserve(1); ok {
		t.Fatal("vacuum accepted traffic reservation")
	}
	repository.endMaintenance()
	if _, ok := repository.Reserve(11); ok {
		t.Fatal("oversized reservation accepted")
	}
	negative, ok := repository.Reserve(-1)
	if !ok {
		t.Fatal("negative reservation was not normalized")
	}
	negative.Cancel()
}

func TestLookupBoundariesMissingValuesRetentionAndClose(t *testing.T) {
	repository := openTestRepository(t, Options{})
	exchange := testExchange("one", time.Unix(1, 0), []byte("body"))
	persistTestExchange(t, repository, exchange)
	found, err := repository.ContainsBoundary(context.Background(), Boundary{CompletedAt: exchange.CompletedAt, ID: exchange.ID})
	if err != nil || !found {
		t.Fatalf("existing boundary found=%v err=%v", found, err)
	}
	found, err = repository.ContainsBoundary(context.Background(), Boundary{CompletedAt: exchange.CompletedAt, ID: "missing"})
	if err != nil || found {
		t.Fatalf("missing boundary found=%v err=%v", found, err)
	}
	if _, found, err := repository.Get(context.Background(), "missing"); err != nil || found {
		t.Fatalf("missing detail found=%v err=%v", found, err)
	}
	if _, found, err := repository.Body(context.Background(), "missing", false); err != nil || found {
		t.Fatalf("missing body found=%v err=%v", found, err)
	}
	items, more, err := repository.List(context.Background(), nil, 0)
	if err != nil || more || len(items) != 0 {
		t.Fatalf("zero page items=%v more=%v err=%v", items, more, err)
	}
	repository.SetRetentionDays(-1)
	status, err := repository.Status(context.Background())
	if err != nil || status.RetentionDays != 0 {
		t.Fatalf("retention status=%+v err=%v", status, err)
	}
	if _, err := repository.CleanupPreview(context.Background(), -1); err == nil {
		t.Fatal("negative cleanup accepted")
	}
	if err := repository.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := repository.Reserve(1); ok {
		t.Fatal("closed repository accepted reservation")
	}
}

func TestDeleteRemovesOnePersistedExchange(t *testing.T) {
	repository := openTestRepository(t, Options{})
	persistTestExchange(t, repository, testExchange("remove", time.Unix(1, 0), []byte("body")))
	persistTestExchange(t, repository, testExchange("keep", time.Unix(2, 0), nil))

	deleted, err := repository.Delete(context.Background(), "remove")
	if err != nil || !deleted {
		t.Fatalf("delete result=%v err=%v", deleted, err)
	}
	if _, found, err := repository.Get(context.Background(), "remove"); err != nil || found {
		t.Fatalf("removed exchange found=%v err=%v", found, err)
	}
	if _, found, err := repository.Get(context.Background(), "keep"); err != nil || !found {
		t.Fatalf("unrelated exchange found=%v err=%v", found, err)
	}
	deleted, err = repository.Delete(context.Background(), "remove")
	if err != nil || deleted {
		t.Fatalf("repeated delete result=%v err=%v", deleted, err)
	}
}

func TestClearRemovesAllPersistedExchanges(t *testing.T) {
	repository := openTestRepository(t, Options{})
	persistTestExchange(t, repository, testExchange("one", time.Unix(1, 0), []byte("body")))
	persistTestExchange(t, repository, testUDPExchange("two", time.Unix(2, 0), []byte("udp")))

	cleared, err := repository.Clear(context.Background())
	if err != nil || cleared != 2 {
		t.Fatalf("clear result=%d err=%v", cleared, err)
	}
	status, err := repository.Status(context.Background())
	if err != nil || status.ExchangeCount != 0 {
		t.Fatalf("status after clear count=%d err=%v", status.ExchangeCount, err)
	}
	var details int
	if err := repository.database.QueryRow(`SELECT count(*) FROM udp_datagrams`).Scan(&details); err != nil || details != 0 {
		t.Fatalf("UDP details after clear=%d err=%v", details, err)
	}
}

func TestReservationCommitIsIdempotentAndFlushesDuringClose(t *testing.T) {
	repository := openTestRepository(t, Options{})
	reservation, ok := repository.Reserve(1)
	if !ok {
		t.Fatal("reservation rejected")
	}
	exchange := testExchange("once", time.Unix(1, 0), nil)
	if err := reservation.Commit(exchange); err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(exchange); err != nil {
		t.Fatal(err)
	}
	if err := repository.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := repository.Status(context.Background())
	if err != nil || status.Writes != 1 {
		t.Fatalf("writes=%d err=%v", status.Writes, err)
	}
	late, ok := repository.Reserve(1)
	if !ok {
		t.Fatal("late reservation rejected")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := repository.Close(closeContext); err == nil {
		t.Fatal("close unexpectedly completed with outstanding reservation")
	}
	if err := late.Commit(exchange); err != nil {
		t.Fatalf("closing commit error=%v", err)
	}
	if err := repository.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCloseFlushesAdmittedWriteWithoutWaitingForBatchInterval(t *testing.T) {
	repository := openTestRepository(t, Options{FlushInterval: time.Hour})
	exchange := testExchange("shutdown-flush", time.Unix(1, 0), []byte("body"))
	reservation, ok := repository.Reserve(4)
	if !ok {
		t.Fatal("reservation rejected")
	}
	if err := reservation.Commit(exchange); err != nil {
		t.Fatal(err)
	}

	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := repository.Close(closeContext); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted, err := Open(Options{Path: repository.options.Path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close(context.Background()) })
	page, _, err := restarted.List(context.Background(), nil, 10)
	if err != nil || ids(page) != "shutdown-flush" {
		t.Fatalf("persisted exchanges=%s err=%v", ids(page), err)
	}
}

func TestCleanupUsesStrictUTCCutoffAndMaintenanceIsExclusive(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.FixedZone("test", 2*60*60))
	repository := openTestRepository(t, Options{Now: func() time.Time { return now }, CleanupBatchSize: 1})
	cutoff := now.UTC().AddDate(0, 0, -1)
	persistTestExchange(t, repository, testUDPExchange("older", cutoff.Add(-time.Nanosecond), []byte("udp")))
	persistTestExchange(t, repository, testExchange("boundary", cutoff, nil))
	persistTestExchange(t, repository, testExchange("newer", cutoff.Add(time.Nanosecond), nil))
	preview, err := repository.CleanupPreview(context.Background(), 1)
	if err != nil || !preview.Cutoff.Equal(cutoff) || preview.ExchangeCount != 1 {
		items, _, listErr := repository.List(context.Background(), nil, 10)
		status, statusErr := repository.Status(context.Background())
		t.Logf("items=%s listErr=%v status=%+v statusErr=%v", ids(items), listErr, status, statusErr)
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if err := repository.beginMaintenance("compact"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Cleanup(context.Background(), 1); err != ErrMaintenanceBusy {
		t.Fatalf("concurrent maintenance error=%v", err)
	}
	repository.endMaintenance()
	result, err := repository.Cleanup(context.Background(), 1)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("cleanup=%+v err=%v", result, err)
	}
	items, _, err := repository.List(context.Background(), nil, 10)
	if err != nil || ids(items) != "newer,boundary" {
		t.Fatalf("retained=%s err=%v", ids(items), err)
	}
	var details int
	if err := repository.database.QueryRow(`SELECT count(*) FROM udp_datagrams`).Scan(&details); err != nil || details != 0 {
		t.Fatalf("UDP details after retention=%d err=%v", details, err)
	}
}

func TestCompactVacuumStatusAndHealth(t *testing.T) {
	repository := openTestRepository(t, Options{})
	for index := 0; index < 20; index++ {
		persistTestExchange(t, repository, testExchange(fmt.Sprint(index), time.Unix(int64(index+1), 0), bytes.Repeat([]byte("x"), 4096)))
	}
	if _, err := repository.Cleanup(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	compact, err := repository.Compact(context.Background())
	if err != nil || compact.FreePagesAfter > compact.FreePagesBefore {
		t.Fatalf("compact=%+v err=%v", compact, err)
	}
	if err := repository.Vacuum(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := repository.Status(context.Background())
	if err != nil || status.Maintenance != "" || status.QueueCapacity <= 0 || status.PageSize <= 0 || status.Writes != 20 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestCleanupCancellationAndVacuumDrainRecovery(t *testing.T) {
	repository := openTestRepository(t, Options{})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.Cleanup(cancelled, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup cancellation error=%v", err)
	}
	reservation, ok := repository.Reserve(1)
	if !ok {
		t.Fatal("reservation rejected")
	}
	vacuumContext, stopVacuum := context.WithTimeout(context.Background(), time.Millisecond)
	defer stopVacuum()
	if err := repository.Vacuum(vacuumContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("vacuum drain error=%v", err)
	}
	reservation.Cancel()
	if next, ok := repository.Reserve(1); !ok {
		t.Fatal("admission did not recover after failed vacuum")
	} else {
		next.Cancel()
	}
}

func TestWriterReportsBusyFailure(t *testing.T) {
	writeResult := make(chan error, 1)
	repository := openTestRepository(t, Options{BusyTimeout: 10 * time.Millisecond, OnWrite: func(_ string, err error) { writeResult <- err }})
	connection, err := repository.database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	reservation, ok := repository.Reserve(1)
	if !ok {
		t.Fatal("reservation rejected")
	}
	if err := reservation.Commit(testUDPExchange("busy", time.Now(), []byte("udp"))); err != nil {
		t.Fatal(err)
	}
	if err := repository.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	status, err := repository.Status(context.Background())
	if err != nil || status.WriteFailures != 1 || status.BusyFailures != 1 || status.LastError == "" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if callbackError := <-writeResult; callbackError == nil {
		t.Fatal("writer failure callback reported success")
	}
}

func TestExchangeRoundTripProperty(t *testing.T) {
	repository := openTestRepository(t, Options{})
	sequence := 0
	property := func(requestBody, responseBody []byte) bool {
		sequence++
		id := fmt.Sprintf("property-%d", sequence)
		exchange := testExchange(id, time.Unix(int64(sequence+1), 0), requestBody)
		exchange.Response.Body = append([]byte(nil), responseBody...)
		exchange.Response.ObservedBodyBytes = int64(len(responseBody))
		persistTestExchange(t, repository, exchange)
		actualRequest, found, err := repository.Body(context.Background(), id, false)
		if err != nil || !found || !bytes.Equal(actualRequest, requestBody) {
			t.Logf("request id=%s got=%x want=%x found=%v err=%v", id, actualRequest, requestBody, found, err)
			return false
		}
		actualResponse, found, err := repository.Body(context.Background(), id, true)
		if err != nil || !found || !bytes.Equal(actualResponse, responseBody) {
			t.Logf("response id=%s got=%x want=%x found=%v err=%v", id, actualResponse, responseBody, found, err)
		}
		return err == nil && found && bytes.Equal(actualResponse, responseBody)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatal(err)
	}
}

func TestUDPDatagramRoundTripProperty(t *testing.T) {
	repository := openTestRepository(t, Options{})
	sequence := 0
	property := func(payload []byte) bool {
		sequence++
		exchange := testUDPExchange(fmt.Sprintf("udp-property-%d", sequence), time.Unix(int64(sequence+1), 0), payload)
		persistTestExchange(t, repository, exchange)
		actual, found, err := repository.Body(context.Background(), exchange.ID, false)
		return err == nil && found && bytes.Equal(actual, payload)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatal(err)
	}
}

func FuzzDecodeExchange(f *testing.F) {
	valid, err := json.Marshal(persistedExchange{Exchange: testExchange("seed", time.Unix(1, 0), []byte("body"))})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"exchange":{"id":"partial"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		exchange, err := decodeExchange(data)
		if err == nil {
			_ = exchange.Clone()
		}
	})
}

func FuzzUDPDatagramBodyRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0xff, 'u', 'd', 'p'})
	f.Fuzz(func(t *testing.T, payload []byte) {
		repository := openTestRepository(t, Options{})
		exchange := testUDPExchange("udp-fuzz", time.Unix(1, 0), payload)
		persistTestExchange(t, repository, exchange)
		actual, found, err := repository.Body(context.Background(), exchange.ID, false)
		if err != nil || !found || !bytes.Equal(actual, payload) {
			t.Fatalf("payload=%x actual=%x found=%v err=%v", payload, actual, found, err)
		}
	})
}

func BenchmarkRepositorySustainedWrites(b *testing.B) {
	repository, err := Open(Options{Path: filepath.Join(b.TempDir(), "benchmark.db"), QueueCount: 512, QueueBytes: 64 << 20})
	if err != nil {
		b.Fatal(err)
	}
	defer repository.Close(context.Background())
	payload := bytes.Repeat([]byte("x"), 1024)
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		reservation, ok := repository.Reserve(int64(len(payload)))
		if !ok {
			if err := repository.WaitIdle(context.Background()); err != nil {
				b.Fatal(err)
			}
			reservation, ok = repository.Reserve(int64(len(payload)))
			if !ok {
				b.Fatal("writer reservation exhausted after drain")
			}
		}
		if err := reservation.Commit(testExchange(fmt.Sprint(index), time.Unix(int64(index+1), 0), payload)); err != nil {
			b.Fatal(err)
		}
	}
	if err := repository.WaitIdle(context.Background()); err != nil {
		b.Fatal(err)
	}
}

func openTestRepository(t *testing.T, options Options) *Repository {
	t.Helper()
	if options.Path == "" {
		options.Path = filepath.Join(t.TempDir(), "history.db")
	}
	repository, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close(context.Background()) })
	return repository
}

func persistTestExchange(t *testing.T, repository *Repository, exchange capture.Exchange) {
	t.Helper()
	bytes := int64(len(exchange.Request.Body))
	if exchange.Datagram != nil {
		bytes += int64(len(exchange.Datagram.Payload))
	}
	if exchange.Response != nil {
		bytes += int64(len(exchange.Response.Body))
	}
	reservation, ok := repository.Reserve(bytes)
	if !ok {
		t.Fatal("reservation rejected")
	}
	if err := reservation.Commit(exchange); err != nil {
		t.Fatal(err)
	}
	if err := repository.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func testExchange(id string, completed time.Time, requestBody []byte) capture.Exchange {
	completed = completed.UTC()
	decoded := int64(len(requestBody))
	return capture.Exchange{
		ID: id, Mode: config.ModeProxy, State: capture.StateCompleted, Revision: 3, ConfigRevision: 2,
		StartedAt: completed.Add(-time.Second), CompletedAt: completed, Duration: time.Second,
		Configuration: capture.ConfigSnapshot{Mode: config.ModeProxy, Revision: 2},
		Request:       capture.Request{Method: http.MethodPost, Path: "/value", Headers: http.Header{"X-Test": {"one", "two"}}, Body: append([]byte(nil), requestBody...), ObservedBodyBytes: int64(len(requestBody)), BodyComplete: true, DecodedPreviewBytes: &decoded},
		Response:      &capture.Response{Status: http.StatusCreated, Headers: http.Header{"Set-Cookie": {"a=1", "b=2"}}, Body: []byte("response"), ObservedBodyBytes: 8, BodyComplete: true, Origin: "upstream"},
	}
}

func testUDPExchange(id string, completed time.Time, payload []byte) capture.Exchange {
	completed = completed.UTC()
	return capture.Exchange{
		ID: id, Transport: capture.TransportUDP, Mode: config.ModeCapture, State: capture.StateCompleted, Revision: 2, ConfigRevision: 1,
		StartedAt: completed.Add(-time.Second), CompletedAt: completed, Duration: time.Second,
		Configuration: capture.ConfigSnapshot{Mode: config.ModeCapture, Revision: 1},
		Datagram:      &capture.Datagram{SourceAddress: "127.0.0.1:10001", LocalAddress: "127.0.0.1:9000", AcceptedBytes: int64(len(payload)), Payload: append([]byte(nil), payload...), PayloadComplete: true, Preview: []byte("udp"), Delivery: capture.DatagramDelivery{Result: "captured"}},
	}
}

func createLegacyDatabase(t *testing.T, path string, exchange capture.Exchange) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		CREATE TABLE exchanges (id TEXT PRIMARY KEY, completed_ns INTEGER NOT NULL, started_ns INTEGER NOT NULL, method TEXT NOT NULL, path TEXT NOT NULL, mode TEXT NOT NULL, status INTEGER, state TEXT NOT NULL, duration_ns INTEGER NOT NULL, request_bytes INTEGER NOT NULL, response_bytes INTEGER NOT NULL, metadata BLOB NOT NULL, request_body BLOB NOT NULL, response_body BLOB);
		CREATE INDEX exchanges_completed_id ON exchanges(completed_ns DESC, id DESC);
		INSERT INTO schema_migrations(version, applied_at) VALUES (1, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	metadataExchange := exchange.Clone()
	metadataExchange.Request.Body = nil
	metadataExchange.Response.Body = nil
	metadata, err := json.Marshal(persistedExchange{Exchange: metadataExchange, Configuration: exchange.Configuration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO exchanges (id, completed_ns, started_ns, method, path, mode, status, state, duration_ns, request_bytes, response_bytes, metadata, request_body, response_body) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, exchange.ID, exchange.CompletedAt.UnixNano(), exchange.StartedAt.UnixNano(), exchange.Request.Method, exchange.Request.Path, exchange.Mode, exchange.Response.Status, exchange.State, exchange.Duration, len(exchange.Request.Body), len(exchange.Response.Body), metadata, exchange.Request.Body, exchange.Response.Body); err != nil {
		t.Fatal(err)
	}
}

func ids(items []capture.Exchange) string {
	result := ""
	for index, exchange := range items {
		if index > 0 {
			result += ","
		}
		result += exchange.ID
	}
	return result
}
