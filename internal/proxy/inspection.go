package proxy

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

const maximumExpansionRatio int64 = 100

func inspect(body []byte, encoding string, settings config.Preview, hardLimit int64) capture.Inspection {
	if strings.TrimSpace(encoding) == "" || strings.EqualFold(strings.TrimSpace(encoding), "identity") {
		preview, truncated := inspectionPrefix(body, settings)
		return capture.Inspection{Preview: preview, PreviewTruncated: truncated}
	}
	decoded, err := decodeEncodings(body, encoding, hardLimit)
	if err != nil {
		preview, truncated := inspectionPrefix(body, settings)
		return capture.Inspection{Preview: preview, PreviewTruncated: truncated, Error: err.Error()}
	}
	preview, truncated := inspectionPrefix(decoded, settings)
	size := int64(len(preview))
	return capture.Inspection{Preview: preview, PreviewTruncated: truncated, DecodedPreviewBytes: &size}
}

func decodeEncodings(body []byte, raw string, limit int64) ([]byte, error) {
	encodings := parseEncodings(raw)
	if len(encodings) == 0 {
		return nil, errors.New("content encoding is empty")
	}
	current := append([]byte(nil), body...)
	for index := len(encodings) - 1; index >= 0; index-- {
		decoded, err := decodeOne(current, encodings[index], limit)
		if err != nil {
			return nil, err
		}
		current = decoded
	}
	return current, nil
}

func parseEncodings(raw string) []string {
	parts := strings.Split(raw, ",")
	encodings := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name != "" && name != "identity" {
			encodings = append(encodings, name)
		}
	}
	return encodings
}

func decodeOne(encoded []byte, encoding string, limit int64) ([]byte, error) {
	var reader io.ReadCloser
	var err error
	switch encoding {
	case "gzip", "x-gzip":
		reader, err = gzip.NewReader(bytes.NewReader(encoded))
	case "deflate":
		reader, err = zlib.NewReader(bytes.NewReader(encoded))
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", encoding)
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s preview: %w", encoding, err)
	}
	allowed := limit
	ratioBound := false
	if ratioLimit := int64(len(encoded)) * maximumExpansionRatio; ratioLimit < allowed {
		allowed = ratioLimit
		ratioBound = true
	}
	if allowed < 1 {
		allowed = 1
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, allowed+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("decode %s preview: %w", encoding, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s preview: %w", encoding, closeErr)
	}
	if int64(len(data)) > allowed {
		if ratioBound {
			return nil, errors.New("decoded preview exceeds expansion ratio")
		}
		return nil, errors.New("decoded preview exceeds configured limit")
	}
	return data, nil
}

func inspectionPrefix(body []byte, settings config.Preview) ([]byte, bool) {
	if !settings.Enabled || int64(len(body)) <= settings.Bytes {
		return append([]byte(nil), body...), false
	}
	return append([]byte(nil), body[:settings.Bytes]...), true
}
