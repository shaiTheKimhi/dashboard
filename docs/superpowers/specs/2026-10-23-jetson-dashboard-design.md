# Jetson browser dashboard: proposed design

Status: implementation authorized by the user and delivered on 2026-10-23. This document records the original design; see [acceptance and deployment decisions](../../acceptance.md) for actual results and the user-service installation.

## Device and constraints

Read-only inspection on 2026-10-23 found:

- NVIDIA Jetson Nano Developer Kit, ARM64, approximately 4 GB RAM.
- Ubuntu 18.04.6, NVIDIA L4T R32.7.1, kernel 4.9.253-tegra, systemd 237.
- Python 3.6.9 and Node 8.10.0; Go and application dependencies are absent.
- Empty project directory, no Git repository or applicable AGENTS.md found.
- `/usr/bin/tegrastats` produces readings as the current unprivileged user.
- `/sys/devices/gpu.0/load` and named CPU/GPU thermal zones are readable.

Do not replace the operating system, JetPack, system Python, or system Node. Runtime compatibility must be demonstrated on this Nano before the full application is implemented.

## Intent and scope

Provide one lightweight, responsive page accessible from another computer or phone. It shows current system health and lets its owner navigate an explicitly shared directory and download individual files.

The first release includes device model, hostname, OS/L4T/kernel versions, uptime, CPU utilization, GPU utilization, RAM/swap, CPU/GPU temperatures, disk capacity, interface addresses and network transfer rates. Show unavailable measurements as unavailable, never as zero. CPU/GPU frequencies and power readings may be added later if needed.

The file view includes breadcrumbs, parent navigation, filename, type, size, modification time, and individual downloads. It handles empty folders, unreadable entries, missing files, and a disconnected device clearly.

Uploads, deletion, editing, shell access, process control, reboot controls, previews, folder ZIP downloads, historical storage, and cloud services are outside this release. Downloaded HTML and other executable content must be attachments, never rendered within the dashboard origin.

## Approaches considered

| Approach | Benefit | Cost or constraint |
| --- | --- | --- |
| Go standard library service + plain browser assets | One ARM64 binary, no runtime packages or frontend build, native HTTP/TLS and rooted filesystem APIs | Requires a Go build toolchain somewhere and a short on-device compatibility check |
| Python service + plain browser assets | Familiar ecosystem for Linux monitoring | Installed Python is too old for current Flask; needs an isolated maintained runtime or obsolete dependencies |
| Adapt a general administration application | Reuses an existing administration interface | Must first verify old ARM64 platform support and add or adapt the Jetson readings and restricted file view |

Recommended default: Go, chosen by the plan author rather than confirmed by the user. Build with a currently supported, patched Go release; the APIs used require at least Go 1.24. Target `linux/arm64`, `GOARM64=v8.0`, `CGO_ENABLED=0`. Embed the page assets in the binary. No external Go modules, Node build, database, container, or reverse proxy is needed.

Go documents a Linux kernel baseline below this device's 4.9 kernel, but an actual binary smoke test remains required. See [Go platform requirements](https://go.dev/wiki/MinimumRequirements). Current Flask requires a newer Python than the installed version; see [Flask installation](https://flask.palletsprojects.com/en/stable/installation/).

## Proposed user experience

One page has an overview of current readings followed by a Files section. On narrow screens, cards stack and the file table can scroll horizontally. Values carry explicit units; connection status and last update time remain visible.

Refresh metrics every 2 seconds with at most one browser request in flight. Stop polling in a hidden tab and refresh immediately when it becomes visible. Keep the last successful values during errors, visibly marked stale after 6 seconds. File listings load only on navigation or explicit refresh. Use native browser downloads rather than fetching whole files into JavaScript memory.

Support keyboard navigation, labelled buttons, visible focus, and status text that does not rely on color. Render filenames with `textContent`; use `URLSearchParams` for paths. All assets are local, so the page works without an internet connection.

## Architecture and metrics

`Browser -- authenticated HTTPS --> Go service -- read-only --> /proc, /sys, shared directory`

The service samples once every 2 seconds and shares one mutex-protected snapshot across clients. Request handlers never start sampling subprocesses. Stop the sampler on service shutdown.

| Reading | Source and interpretation |
| --- | --- |
| Device identity | `/proc/device-tree/model` with NUL trimming, `/etc/os-release`, `/etc/nv_tegra_release`, hostname and kernel |
| Uptime and CPU | `/proc/uptime`, deltas of `/proc/stat`; avoid double-counting guest counters; first utilization sample is unavailable |
| RAM and swap | `/proc/meminfo`; used RAM = MemTotal - MemAvailable; label shared system RAM rather than dedicated GPU memory |
| GPU load | `/sys/devices/gpu.0/load`; verify the driver's 0–1000 scale against `tegrastats` before displaying percent |
| Temperature | Discover `/sys/class/thermal/thermal_zone*/type`; identify CPU/GPU by type, convert millidegrees Celsius |
| Disk | `statfs` for `/` and the configured file root; distinguish total free from available-to-user bytes |
| Network | Interface names/addresses and deltas of `/proc/net/dev`; ignore loopback, reset baselines after counter reset or interface changes |

Use a monotonic clock for rate calculations. Missing optional sensors yield null values and warnings while other readings continue. A failed sampling cycle preserves the last good snapshot and its original timestamp. `tegrastats` is a validation tool, not an additional long-running process. NVIDIA describes its output in the [L4T tegrastats guide](https://docs.nvidia.com/jetson/l4t/Tegra%20Linux%20Driver%20Package%20Development%20Guide/AppendixTegraStats.html).

## File access boundary

Proposed initial root: `/home/shai/dashboard-share`, a dedicated directory containing only files intended for download. The root is configurable, but never defaults to `/` or the entire home directory. The service account gets read/search access through a dedicated group, without write permission. Verify permissions for files created later as well as existing files.

Use one `os.Root` opened at startup for all user-selected filesystem operations. Reject absolute paths, `..` components, NULs, malformed encoding, duplicate path parameters, and paths longer than 4096 bytes. Treat the empty query path as the root. Decode query values once. Allow relative symlinks only when their targets stay inside the shared tree; reject escaping and absolute symlinks. Rooted operations must enforce the boundary even if a symlink changes during a request. See [Go's traversal-resistant APIs](https://go.dev/blog/osroot).

Everything readable inside the export root is shareable, including hidden files: this keeps the authorization rule simple and prevents hidden-file filters from being mistaken for a security boundary. Explain this during setup. Do not place credentials there. Administrators must not bind-mount sensitive trees or introduce sensitive hard links into the export; `os.Root` does not isolate those objects.

List at most 1000 entries per folder, detect one additional entry, and return an explicit `truncated` flag. Sort the returned subset with directories first and then by name; do not imply it is the complete alphabetically first 1000 entries. A truncated listing explains the limit. Pagination and recursive search can be added if real directories require them.

Open downloads read-only and nonblocking, then inspect the opened descriptor and allow regular files only. Reject directories, FIFOs, sockets and devices; a FIFO must not hang a request. Stream the opened descriptor through `http.ServeContent` for bounded memory, HEAD and byte-range support. Generate a safe attachment filename, force `application/octet-stream`, and set `X-Content-Type-Options: nosniff`. Files being written by another application are best-effort downloads, not consistent snapshots.

## API contract

All routes require authentication, including static assets and health checks. All data responses use `Cache-Control: no-store`. Unsupported methods return 405.

| Route | Result |
| --- | --- |
| `GET /` | Embedded page |
| `GET /static/{asset}` | Only known embedded assets |
| `GET /api/system` | Identity, `sampled_at` in RFC3339 UTC, `stale`, metrics, and `warnings` |
| `GET /api/files?path=<relative>` | `path`, `parent` (null at root), `entries`, `truncated`; each entry has `name`, `kind`, `size_bytes`, `modified_at` |
| `GET` or `HEAD /api/download?path=<relative>` | Attachment stream; supports Range |
| `GET /healthz` | Liveness; does not depend on optional sensor availability |

Metric units are encoded in field names (`cpu_percent`, `gpu_percent`, `ram_used_bytes`, `cpu_temp_c`, `rx_bytes_per_second`). API errors have `{ "error": { "code": "...", "message": "..." } }`; use 400 for invalid input, 403 for access denied, 404 for disappeared/missing entries, and 500 for unexpected internal failures. Avoid returning absolute host paths or raw OS errors. Disk and network readings are arrays, identified by mount path and interface name respectively.

## Access and deployment

Assumption awaiting user preference: private local-network access by one owner. No public port forwarding is included.

Use HTTP Basic authentication with username `dashboard` and a generated 32-byte random secret; store only its SHA-256 digest in a protected configuration file and use constant-time comparison. This design is for a machine-generated high-entropy secret, not a human-chosen password. The browser supplies its standard sign-in dialog; credential rotation requires updating configuration and restarting the service. A custom login and explicit logout are outside this release.

Default listener: `127.0.0.1:8080`, accessible through SSH forwarding for initial verification. A LAN deployment sets a specific private interface address on port 8443 and requires a TLS certificate and key; startup must refuse plaintext non-loopback binding. Use an operator-trusted local certificate with the chosen hostname/IP in its SAN, and document client trust setup. Do not transmit the secret over unencrypted LAN HTTP. If the user chooses remote access, extend this through their VPN and reassess listener/certificate configuration.

Install one binary under `/opt/jetson-dashboard`, configuration and TLS material outside the export under `/etc/jetson-dashboard`, and a systemd service running as dedicated user `jetson-dashboard`. Use directives available in systemd 237: automatic restart, `NoNewPrivileges`, `PrivateTmp`, `PrivateDevices`, `ProtectSystem=strict`, and `ProtectHome=read-only`. Ensure these settings still allow the selected export and telemetry reads. Log to journald without credentials or query strings. No application writes or privileged shell commands at runtime.

Set finite header/idle timeouts and header size limits. Avoid a short global write timeout that aborts large downloads. Limit concurrent downloads to 2 and return 429 for excess requests. Graceful shutdown closes listeners and cancels ongoing work after a documented 10-second grace period.

## Acceptance and remaining choices

Acceptance: authenticated access from a second LAN device; live readings matching local tools within sampling differences; navigation and byte-identical large downloads; no file escape; useful stale/error states; normal operation after reboot; no dependency on internet access.

Resource targets to measure, not promises: under 100 MiB RSS and under 5% of one CPU core averaged over 5 minutes with one visible client, outside download activity. Download memory must not grow with file size. Test against the Nano's normal workloads.

User choices still open: LAN versus VPN/public access, the actual shared directory, and any preferred implementation language. Defaults above make this plan reviewable without requiring those answers first. Adding direct internet access changes the deployment scope and needs a revised access design.
