# UDP security and operations

UDP is disabled by default and binds to `0.0.0.0:12050` when enabled. Do not expose it beyond a trusted network unless the operator accepts unauthenticated datagrams and sensitive-payload retention.

## Threat model and controls

- Reflection and amplification: capture replies are disabled by default; enabled replies are fixed local configuration values and cannot be selected by packet contents. Reply bytes cannot exceed the configured datagram limit.
- Open relay: proxy mode requires one configured unicast `host:port`; broadcast, multicast, and packet-derived destinations are rejected.
- Spoofed peers: UDP has no handshake. Proxy replies use only a bounded, canonical client-address session mapping and expire after the configured TTL.
- Untrusted bytes: each receive uses a configured datagram bound; previews and UI rendering are bounded, while raw body reads use authenticated Range requests.
- Resource exhaustion: session count, TTL, reply rate, reply-byte rate, RAM history, SQLite reservations, writer queue, SSE replay, and subscriber queues are bounded. Over-limit datagrams become visible discarded exchanges.
- Sensitive retention: UDP payloads can contain credentials or personal data. SQLite retention, deletion, access control, and host filesystem permissions remain operator responsibilities.

## Deployment

A container does not publish UDP by default. If intentionally exposing UDP, publish the matching UDP port, for example `-p 12050:12050/udp` with `REQRELAY_UDP_ENABLED=true` and `REQRELAY_UDP_LISTEN=0.0.0.0:12050`. Configure a fixed unicast upstream before enabling proxy mode.

UDP Settings changes hot-reload the UDP ingest plane. Enabling or changing the listener binds the candidate before persistence; an unavailable address or unresolved proxy upstream rejects the complete save and preserves the active endpoint. Every UDP reload closes existing proxy sessions. Monitor `status.udp`, SQLite writer health, history retention, and disk growth before increasing limits.

## Release profile and platforms

`TestUDPIngressLoadProfile` sends 1 KiB UDP payloads at 100 and 200 datagrams per second into SQLite capture mode. It proves the configured RAM cap, zero capture-mode proxy sessions, SQLite queue capacity, and a 2 MiB database-growth ceiling for each one-second profile. Run it with:

```sh
go test ./internal/app -run '^TestUDPIngressLoadProfile$' -count=1
```

This is a bounded local smoke profile, not a network-capacity guarantee. Packet loss, disk latency, CPU contention, network buffers, and downstream client behavior remain deployment-specific.

Release builds are CGO-free for Linux AMD64, ARM64, and ARMv7. CI compiles and runs the complete `internal/app` test binary, including UDP listener/capture/proxy tests, under QEMU for ARM64 and ARMv7. Local cross-compilation proves binary generation but does not replace target-hardware or QEMU execution.
