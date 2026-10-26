# Jetson Dashboard Implementation Plan

Execution status: implemented and installed as a persistent user service. See [acceptance results](../../acceptance.md) for verification and the explicit remaining client/second-device/reboot checks. Original planning checkboxes below are retained as the initial brief; the execution record is in that acceptance document.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. This document is planning only; implementation is a separate user request.

**Goal:** Build a browser-accessible Jetson overview with a restricted, read-only directory browser and individual file downloads.

**Architecture:** One unprivileged Go process embeds the browser assets, samples Linux telemetry, and serves authenticated HTTP/TLS. File operations remain inside one configured export root. systemd starts and supervises the binary.

**Tech Stack:** Maintained Go release with Go 1.24+ APIs; standard library only; HTML, CSS, vanilla JavaScript; systemd 237.

**Spec:** [Proposed design](../specs/2026-10-23-jetson-dashboard-design.md). Read it before execution; its defaults are author recommendations awaiting user review.

## Global Constraints

- Do not replace the operating system, JetPack, system Python, or system Node.
- Target `linux/arm64`, `GOARM64=v8.0`, `CGO_ENABLED=0`.
- No external Go modules, Node build, database, container, or reverse proxy is needed.
- Refresh metrics every 2 seconds with at most one browser request in flight.
- List at most 1000 entries per folder, detect one additional entry, and return an explicit `truncated` flag.
- All routes require authentication, including static assets and health checks.
- Startup must refuse plaintext non-loopback binding.
- Limit concurrent downloads to 2 and return 429 for excess requests.
- Proposed export root: `/home/shai/dashboard-share`; all readable contents are shareable, including hidden files.
- Runtime is read-only and unprivileged. No uploads, editing, deletion, shell or device controls.

## Review Focus

- Path encoding, escaping symlinks and filesystem replacement races must not expose files outside the export; tests belong to task 3.
- Special files and slow/large downloads must not block all clients or allocate memory proportional to file size; task 3.
- Missing sensors, initial CPU samples and resetting network counters must show unavailable values rather than fabricated zero/negative rates; task 2.
- Expired certificates, absent credentials and plaintext LAN binding must fail clearly; task 1 and task 5.
- HTML-like/Unicode filenames, overlapping requests and device disconnection must not produce script injection or a misleading live status; task 4.

## Planned files

| Path | Responsibility |
| --- | --- |
| `go.mod` | Module `jetson-dashboard`, Go API floor |
| `main.go` | Configuration, embedded assets, sampler lifecycle, HTTP server and shutdown |
| `server.go`, `server_test.go` | Routes, authentication, TLS/listener validation, response/error helpers |
| `metrics.go`, `metrics_test.go` | Linux readings, snapshot schema and sampling/delta logic |
| `files.go`, `files_test.go` | Rooted directory listings and streamed downloads |
| `static/index.html`, `static/app.js`, `static/style.css` | Responsive browser interface |
| `deploy/jetson-dashboard.service`, `deploy/dashboard.env.example` | systemd unit and documented configuration |
| `README.md` | Build, configure, certificate trust, start, verify, update and uninstall instructions |

Use one Go package `main`; avoid additional layers. Initialize Git only if requested during implementation. No commits are possible in the current empty, uninitialized workspace.

## Task 1: Prove device compatibility and establish authenticated serving

**Files:** `go.mod`, `main.go`, `server.go`, `server_test.go`, initial `README.md`.

**Interfaces:** `Config` holds `ListenAddr`, `FileRoot`, `SecretSHA256`, `TLSCert`, `TLSKey`. `LoadConfig() (Config, error)` reads `DASHBOARD_LISTEN`, `DASHBOARD_FILE_ROOT`, `DASHBOARD_SECRET_SHA256`, `DASHBOARD_TLS_CERT`, `DASHBOARD_TLS_KEY`. Defaults are `127.0.0.1:8080` and `/home/shai/dashboard-share`; the secret has no default. `NewHandler(cfg Config, metrics func() Snapshot, root *os.Root, assets fs.FS) http.Handler` will wire later tasks; initially implement only auth and health routing until those types exist.

- [ ] Choose and record an exact supported Go patch release from the official downloads; build on another machine or unpack the verified ARM64 toolchain under a project/user-owned directory. Do not install obsolete Ubuntu Go packages or change system toolchains.
- [ ] Write table-driven `TestValidateConfig` and `TestRequireAuth`: missing/malformed digest fails; non-loopback HTTP fails; malformed/unspecified bind addresses fail; numeric loopback HTTP passes; LAN TLS configuration passes; missing, wrong or malformed credentials yield 401 with a Basic challenge; valid credentials reach health; assets and unknown routes also pass through authentication.
- [ ] Run `go test ./...` and observe the expected missing implementation failures. Implement configuration validation, auth, health, and server start using a small temporary health-only mux; compose the final `NewHandler` after tasks 2 and 3. Check TLS files at startup; invalid, mismatched or expired certificates fail with an actionable startup error.
- [ ] Test invalid/mismatched/expired certificates with generated temporary certificates in `TestTLSConfig`; ensure no credential or file contents appear in errors/logs. Compare fixed-length digests in constant time.
- [ ] Run `go test ./...` and `go vet ./...`; build with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 go build -trimpath -o build/jetson-dashboard .`.
- [ ] Run the binary on this Nano bound to loopback and verify a 401 without credentials and a 200 authenticated `/healthz`. Record compiler version and kernel. If it cannot run, resolve compatibility before proceeding.

**Deliverable:** An ARM64 binary demonstrably serving authenticated requests on this device. Do not expose it on the LAN yet.

## Task 2: Collect reliable Linux and Jetson telemetry

**Files:** `metrics.go`, `metrics_test.go`, sampler integration in `main.go`.

**Interfaces:** Define `Snapshot` with identity, `SampledAt time.Time`, `Stale bool`, `Warnings []string`, nullable numeric metrics, disk and interface arrays. Match the JSON field names and units in the spec. `NewCollector(procRoot, sysRoot, etcRoot, exportRoot string) *Collector`; `(*Collector).Sample(now time.Time) (Snapshot, error)` keeps prior counters; `(*Collector).Run(ctx context.Context, interval time.Duration)` updates the cache; `(*Collector).Latest() Snapshot` returns an independently safe snapshot copy and computes stale status. Test roots use temporary fixture directories; production roots are `/proc`, `/sys`, `/etc`.

- [ ] Write `TestSampleLinux`, `TestMissingSensors`, `TestCounterDeltas`, and `TestSnapshotFreshness` with realistic Nano fixture text. Assert MemTotal=1000 kB and MemAvailable=400 kB produce 614400 used bytes; GPU raw 250 produces 25%; thermal raw 36500 produces 36.5 C; CPU initial percent is null; CPU deltas with idle 60/total 100 produce 40%; a 2000-byte RX increase over 2 seconds produces 1000 B/s; counter reset gives null for that interval; stale becomes true after more than 6 seconds.
- [ ] Add assertions for CPU guest-counter handling, interface addition/removal, missing MemAvailable, missing CPU/GPU zones, malformed optional files, zero elapsed intervals, and a failed cycle retaining the last good timestamp. Inject a fake clock or explicit sample times rather than sleeping.
- [ ] Run targeted tests to observe failures; implement bounded reads, type-based sensor discovery, rate calculations, optional values, and the shared snapshot. Keep static identity separate from dynamic counters where useful without adding a general plugin system.
- [ ] Start one sampler on startup with a 2-second ticker, stop it through context cancellation, and expose its `Latest` method to the eventual handler. Do not launch `tegrastats` in the application.
- [ ] Run `go test ./...` and compare device readings with `free`, `df`, `/proc` counters and a short `tegrastats` sample. CPU/memory comparisons allow differing sampling windows and accounting definitions; verify GPU scaling with a nonzero reading when normal GPU work is available. If scaling cannot be confirmed, keep GPU percent unavailable and document the remaining check.

**Deliverable:** A shared metrics snapshot that degrades gracefully when optional readings disappear.

## Task 3: Add the restricted directory browser and streamed downloads

**Files:** `files.go`, `files_test.go`, routing in `server.go`, root lifecycle in `main.go`.

**Interfaces:** `ListFiles(root *os.Root, relative string) (FileListing, error)` returns the spec's listing schema. `OpenDownload(root *os.Root, relative string) (*os.File, fs.FileInfo, error)` returns an already-open regular file. `NewHandler` consumes these functions and `Collector.Latest` for the routes in the spec. The root opens once at startup and closes after handlers stop.

- [ ] Write `TestListFiles`: nested/empty directories, Unicode/spaces/HTML-like names, directory-first ordering, null parent at root, metadata, read errors and an explicit truncated result at 1001 entries. Check only 1001 entries need to be read before stopping.
- [ ] Write `TestFileBoundary`: absolute/parent paths, malformed and double-encoded query strings, duplicate `path` keys, NUL, excessive path length, escaping/absolute symlinks, allowed in-root relative symlinks, prefix-sibling paths and a symlink-swapping stress case. No outside sentinel bytes may be returned. Test hidden files as intentionally shareable.
- [ ] Write `TestDownload`: exact bytes, HEAD, byte ranges, safe Unicode attachment names, 404 after deletion, directory rejection and nonblocking FIFO rejection. Check `application/octet-stream` and `nosniff` even for HTML/SVG files. Add `TestConcurrentDownloads` for two active streams, a third returning 429, and slot release on cancellation. Obtain real permission-denied coverage as an unprivileged user.
- [ ] Run targeted tests to observe failures. Implement query validation and rooted access without an `EvalSymlinks`-then-open pattern. Use `Root.OpenFile` with read-only/nonblocking Linux flags and validate the opened descriptor before serving. No recursive reads or archives.
- [ ] Wire authenticated system/files/download/assets/health routes and consistent errors; reject unsupported methods. Enforce no-store on data responses. Set bounded HTTP header/idle settings and avoid a short global download write timeout.
- [ ] Run `go test ./...`; test `go test -race ./...` on a supported development machine if race tooling is unavailable on the Nano. Transfer a representative large file on the Nano, compare SHA-256 before and after, watch RSS, disconnect mid-transfer, and confirm metrics still respond.

**Deliverable:** Read-only, authenticated browsing and downloads with a tested export boundary and bounded memory.

## Task 4: Build the browser interface

**Files:** `static/index.html`, `static/app.js`, `static/style.css`, asset embedding in `main.go`.

**Interfaces:** Consume only the documented routes and schemas; use same-origin Basic authentication. No browser storage of credentials, frontend dependencies or build tooling. Static assets are embedded at build time.

- [ ] Build overview cards for identity, uptime, CPU/GPU, memory/swap, CPU/GPU temperatures, disks and interface activity; show units and unavailable states. Add last-update and connection status text.
- [ ] Implement recursive-timeout polling with a single request in flight, 2-second cadence, visibility pause/resume, cancellation, and stale-after-6-seconds behavior. Preserve last successful readings when a request fails. Handle 401 with clear sign-in/reload instructions.
- [ ] Implement root/parent/breadcrumb navigation, metadata rows, explicit refresh, empty/error/loading/truncated states and ordinary anchor downloads. Safely encode query values and render all device-provided strings with `textContent`. Ignore or abort obsolete directory requests after rapid navigation.
- [ ] Apply a simple responsive layout, keyboard-accessible controls, visible focus and non-color status messages. No external fonts/CDNs, historical charts, file previews or decorative assets are required.
- [ ] Verify in a desktop browser and a narrow mobile viewport: special filenames render literally, paths with spaces/Unicode download correctly, rapid navigation cannot show the wrong folder, a hidden tab stops polling, offline mode marks data stale, and reconnecting recovers. Record browser/version and results in `README.md`; add no test framework solely for these checks.
- [ ] Run `go test ./...` and rebuild so embedded assets reflect the reviewed files.

**Deliverable:** A complete page serving both requested capabilities from the device.

## Task 5: Package and verify local-network deployment

**Files:** `deploy/jetson-dashboard.service`, `deploy/dashboard.env.example`, `README.md`, final integration in `main.go`.

**Interfaces:** Deployment supplies the task 1 environment variables. Service user is `jetson-dashboard`; binary path is `/opt/jetson-dashboard/jetson-dashboard`; config/TLS paths are under `/etc/jetson-dashboard`. Use a real selected private address for `DASHBOARD_LISTEN=<private-IP>:8443`; placeholders must fail rather than bind all interfaces.

- [ ] Write setup instructions for a dedicated service account/group, export read/search permissions, verified binary install and a protected environment file. Generate the 32-byte secret locally and store its digest; display the secret only to the operator during setup, outside logs and the export root. Explain that every readable file under the export, including hidden files, is shared.
- [ ] Document certificate creation/provisioning with correct SAN and client trust setup; record expiry and renewal procedure. Document loopback HTTP plus SSH forwarding for initial testing and LAN TLS for ordinary use. Do not change firewall/network configuration or expose ports during the planning task.
- [ ] Add the systemd 237-compatible hardening and restart directives from the spec; implement cancellation and 10-second shutdown grace. Provide `systemctl` and `journalctl` instructions, permissions diagnostics, safe binary replacement/rollback and uninstall steps that preserve shared files.
- [ ] During implementation, run `systemd-analyze verify deploy/jetson-dashboard.service` after the referenced executable is installed; check the actual unit as the dedicated account can read metrics and the export, cannot write shared files, and cannot read TLS/config secrets through file routes. Validate new shared files inherit usable read permissions.
- [ ] Complete on-device checks: absent auth yields 401, valid TLS/auth works from another LAN device, expired/untrusted certificate behavior is documented, file hash matches, traversal attacks fail, optional sensor loss leaves the page usable, interrupted downloads release slots, graceful restart recovers, and the service starts after a user-scheduled reboot.
- [ ] Measure RSS and average process CPU for 5 minutes with one visible client, outside downloads. Targets: under 100 MiB RSS and under 5% of one core. Confirm memory stays bounded during a large transfer. Record measured values and limitations rather than claiming targets without evidence.
- [ ] Final required checks: `go test ./...`, `go vet ./...`, ARM64 release build, unit validation and browser/device acceptance record. Run the race suite on a suitable machine once after final code changes. Confirm assets load with external internet disconnected while the LAN remains available.

**Deliverable:** A documented, restartable local dashboard with measured Nano compatibility and an acceptance record.

## Sequence and completion

Execute tasks in order: compatible authenticated service → telemetry → files → browser interface → deployment. Each task has an independently checkable result. Keep any future code commits aligned with these tasks if the user initializes version control.

The plan is complete when reviewed as a proposal. The application is complete only after task 5 acceptance, including actual second-device access and the scheduled reboot check. If a deployment choice or test requires user-owned credentials, certificate trust changes or a reboot window, finish the reviewable artifacts first and identify the exact remaining operational step.
