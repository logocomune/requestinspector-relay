package capture

import (
	"fmt"
	"io"
)

func ReadBody(reader io.Reader, limit int64) ([]byte, int64, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("body limit must be positive")
	}
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, int64(len(data)), fmt.Errorf("read request body: %w", err)
	}
	observed := int64(len(data))
	if observed > limit {
		return data[:limit], observed, ErrBodyTooLarge
	}
	return data, observed, nil
}
