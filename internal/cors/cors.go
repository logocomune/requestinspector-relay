package cors

import "net/http"

const allowedMethods = "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS"

func Handler(enabled func() bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !enabled() || request.Header.Get("Origin") == "" {
			next.ServeHTTP(writer, request)
			return
		}

		origin := request.Header.Get("Origin")
		writer.Header().Set("Access-Control-Allow-Origin", origin)
		if request.Method == http.MethodOptions {
			writer.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
			writer.Header().Set("Access-Control-Allow-Methods", allowedMethods)
			allowHeaders := request.Header.Get("Access-Control-Request-Headers")
			if allowHeaders == "" {
				allowHeaders = "*"
			}
			writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			writer.Header().Set("Access-Control-Max-Age", "3600")
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writer.Header().Set("Vary", "Origin")

		next.ServeHTTP(writer, request)
	})
}
