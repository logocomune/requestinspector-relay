package capture

import (
	"context"
	"net/http"
)

type CaptureHandler struct{}

func (CaptureHandler) Handle(_ context.Context, request Request, snapshot ConfigSnapshot) (ModeResult, error) {
	body := []byte(snapshot.Capture.Body)
	if !captureResponseAllowsBody(request.Method, snapshot.Capture.Status) {
		body = nil
	}
	return ModeResult{
		Response: DownstreamResponse{
			Status:  snapshot.Capture.Status,
			Headers: cloneHeaders(snapshot.Capture.Headers),
			Body:    body,
		},
	}, nil
}

func captureResponseAllowsBody(method string, status int) bool {
	if method == http.MethodHead {
		return false
	}
	return status != http.StatusNoContent && status != http.StatusResetContent && status != http.StatusNotModified
}
