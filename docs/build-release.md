# Release builds

`scripts/build-release.sh` builds the embedded SvelteKit frontend and produces six CGO-free Go binaries under `dist/release/`:

- `reqrelay-linux-amd64`
- `reqrelay-windows-amd64.exe`
- `reqrelay-darwin-amd64`
- `reqrelay-darwin-arm64`
- `reqrelay-linux-armv7`
- `reqrelay-linux-arm64`

The script requires Go and npm. Install frontend dependencies once before running it:

```sh
cd web
npm ci
cd ..
./scripts/build-release.sh
```

The script reads version and commit from Git (`git describe --tags --always --dirty`, `git rev-parse HEAD`) and sets the UTC build timestamp. The `armv7` binary uses `GOARM=7`. ARM64 builds target both Linux and Apple Silicon macOS; ARMv7 targets Linux.

## Docker image

Build the multi-stage image locally:

```sh
docker build \
  --build-arg BUILD_VERSION="$(git describe --tags --always --dirty)" \
  --build-arg BUILD_COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t requestinspector-relay:local .
```

Run with configuration and SQLite persisted under `/data`:

```sh
docker run --rm -p 8080:8080 -p 8081:8081 -v requestinspector-relay-data:/data requestinspector-relay:local
```

The image binds management on port `8080` and inspected traffic on port `8081`. UDP is disabled and unpublished by default; intentional UDP exposure requires both a configured listener and an explicit Docker `/udp` port mapping. The default configuration is `/data/reqrelay.yaml`; the SQLite database is `/data/reqrelay.db`. Both survive container recreation through the named volume.

Both HTTP servers can reload internal listener addresses at runtime, but Docker publishing is external process state. Changing management port `8080` or traffic port `8081` does not update `-p` mappings; publish destination ports in advance or recreate the container with matching mappings.

## GitHub Actions

Pushing a `v*` tag runs GoReleaser. It creates release archives and checksums, then publishes `linux/amd64` and `linux/arm64` images to GHCR with versioned and `latest` manifest tags.

Release archives use stable names so users can download the latest release without discovering its version:

- `reqrelay_linux_x86_64.tar.gz`
- `reqrelay_linux_arm64.tar.gz`
- `reqrelay_linux_armv7.tar.gz`
- `reqrelay_darwin_x86_64.tar.gz`
- `reqrelay_darwin_arm64.tar.gz`
- `reqrelay_windows_x86_64.zip`
- `reqrelay_windows_arm64.zip`

GoReleaser preserves release tags and binary version metadata. Checksums remain available as `checksums.txt`.

Pushing to `develop` runs verification and publishes a nightly GHCR image for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.
