package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type cursorBoundary struct {
	CompletedAt int64  `json:"completed_at_unix_nano"`
	ID          string `json:"id"`
}

func encodeCursor(completedAt time.Time, id string) string {
	data, err := json.Marshal(cursorBoundary{CompletedAt: completedAt.UnixNano(), ID: id})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(value string) (cursorBoundary, error) {
	if value == "" || len(value) > 512 {
		return cursorBoundary{}, errors.New("invalid cursor length")
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursorBoundary{}, errors.New("invalid cursor encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cursor cursorBoundary
	if err := decoder.Decode(&cursor); err != nil {
		return cursorBoundary{}, errors.New("invalid cursor payload")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return cursorBoundary{}, errors.New("invalid cursor payload")
	}
	if cursor.CompletedAt <= 0 || cursor.ID == "" || len(cursor.ID) > 128 {
		return cursorBoundary{}, errors.New("invalid cursor boundary")
	}
	return cursor, nil
}

type byteRange struct {
	start int64
	end   int64
}

func parseRange(value string, size int64) (byteRange, error) {
	if size < 0 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return byteRange{}, errors.New("invalid range")
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
	if len(parts) != 2 || parts[0] == "" && parts[1] == "" {
		return byteRange{}, errors.New("invalid range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 || size == 0 {
			return byteRange{}, errors.New("invalid suffix range")
		}
		if suffix > size {
			suffix = size
		}
		return byteRange{start: size - suffix, end: size - 1}, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return byteRange{}, errors.New("range start is outside body")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return byteRange{}, errors.New("invalid range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return byteRange{start: start, end: end}, nil
}

func contentRange(selected byteRange, size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", selected.start, selected.end, size)
}
