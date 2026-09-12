# Stage 1 — build SvelteKit frontend.
FROM --platform=$BUILDPLATFORM node:24-alpine3.23 AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Stage 2 — build static Go binary for requested target platform.
FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine3.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/internal/webui/dist/ internal/webui/dist/
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG TARGETVARIANT=
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=none
ARG BUILD_DATE=unknown
RUN case "${TARGETARCH}" in \
      arm) export GOARM="${TARGETVARIANT#v}" ;; \
    esac && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" ${GOARM:+GOARM="${GOARM}"} \
    go build -trimpath -ldflags="-s -w -X main.version=${BUILD_VERSION} -X main.commit=${BUILD_COMMIT} -X main.date=${BUILD_DATE}" -o reqrelay ./cmd/reqrelay

# Stage 3 — minimal runtime.
FROM alpine:3.23
COPY --from=builder /app/reqrelay /reqrelay

VOLUME ["/data"]

ENV REQRELAY_MANAGEMENT_LISTEN=0.0.0.0:8080
ENV REQRELAY_TRAFFIC_LISTEN=0.0.0.0:8081
ENV REQRELAY_CONFIG=/data/reqrelay.yaml
ENV REQRELAY_SQLITE_PATH=/data/reqrelay.db

COPY docs/reqrelay.example.yaml /data/reqrelay.yaml

EXPOSE 8080
EXPOSE 8081

ENTRYPOINT ["/reqrelay"]
