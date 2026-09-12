package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/logocomune/requestinspector-relay/internal/events"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
)

type cleanupRequest struct {
	KeepDays int  `json:"keep_days"`
	Confirm  bool `json:"confirm"`
}

type vacuumRequest struct {
	Confirm bool `json:"confirm"`
}

func (api *handler) requireSQLite(writer http.ResponseWriter) bool {
	if api.persistent == nil {
		writeError(writer, http.StatusConflict, "sqlite_disabled", "SQLite persistence is not active.")
		return false
	}
	return true
}

func (api *handler) sqliteStatus(writer http.ResponseWriter, request *http.Request) {
	if !api.requireSQLite(writer) {
		return
	}
	status, err := api.persistent.Status(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "sqlite_status_failed", "SQLite status could not be read.")
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (api *handler) sqliteCleanupPreview(writer http.ResponseWriter, request *http.Request) {
	if !api.requireSQLite(writer) {
		return
	}
	keepDays, err := strconv.Atoi(request.URL.Query().Get("keep_days"))
	if err != nil || keepDays < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_keep_days", "keep_days must be a non-negative integer.")
		return
	}
	preview, err := api.persistent.CleanupPreview(request.Context(), keepDays)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "cleanup_preview_failed", "Cleanup preview could not be calculated.")
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (api *handler) sqliteCleanup(writer http.ResponseWriter, request *http.Request) {
	if !api.requireSQLite(writer) {
		return
	}
	var input cleanupRequest
	if err := decodeJSONBody(writer, request, &input); err != nil || input.KeepDays < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_cleanup", "Cleanup request is invalid.")
		return
	}
	if !input.Confirm {
		writeError(writer, http.StatusPreconditionFailed, "confirmation_required", "Cleanup requires explicit confirmation.")
		return
	}
	result, err := api.persistent.Cleanup(request.Context(), input.KeepDays)
	if err != nil {
		api.writeMaintenanceError(writer, err, "cleanup_failed")
		return
	}
	for _, removed := range api.repository.RemoveCompletedBefore(result.Cutoff) {
		api.events.Publish(events.Envelope{Type: events.ExchangeEvicted, ExchangeID: removed.ID, Revision: removed.Revision})
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *handler) sqliteCompact(writer http.ResponseWriter, request *http.Request) {
	if !api.requireSQLite(writer) {
		return
	}
	result, err := api.persistent.Compact(request.Context())
	if err != nil {
		api.writeMaintenanceError(writer, err, "compact_failed")
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *handler) sqliteVacuum(writer http.ResponseWriter, request *http.Request) {
	if !api.requireSQLite(writer) {
		return
	}
	var input vacuumRequest
	if err := decodeJSONBody(writer, request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_vacuum", "Vacuum request is invalid.")
		return
	}
	if !input.Confirm {
		writeError(writer, http.StatusPreconditionFailed, "confirmation_required", "Full vacuum requires explicit confirmation.")
		return
	}
	if err := api.persistent.Vacuum(request.Context()); err != nil {
		api.writeMaintenanceError(writer, err, "vacuum_failed")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"completed": true})
}

func (api *handler) writeMaintenanceError(writer http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, requestsqlite.ErrMaintenanceBusy):
		writeError(writer, http.StatusConflict, "maintenance_in_progress", "Another SQLite maintenance operation is active.")
	case errors.Is(err, requestsqlite.ErrDiskSpace):
		writeError(writer, http.StatusInsufficientStorage, "insufficient_disk_space", "Full vacuum requires at least twice the current database size in free space.")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(writer, http.StatusRequestTimeout, "maintenance_cancelled", "SQLite maintenance was cancelled.")
	default:
		writeError(writer, http.StatusInternalServerError, fallback, "SQLite maintenance failed.")
	}
}
