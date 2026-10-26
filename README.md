# Jetson dashboard

A small browser dashboard for the Jetson Nano with live monitoring, file browsing, creation/deletion, downloads and simple text editing. One Go binary serves live system readings and a restricted directory browser. It has no application dependencies, CDN requests, database or frontend build step.

## Installed on this device

- Address: **https://192.168.68.107:8443** (on the same local network).
- Username and password: read `/home/shai/.config/jetson-dashboard/credentials.txt` locally. This private file is outside the shared directory; do not commit or share it.
- Shared files: **`/home/shai`**, expanded at the owner's request on 2026-10-24. Browse subfolders and download readable files, including hidden files. This includes private configuration and credentials under the home directory, so anyone signed in has access to them.
- Service: `systemctl --user status jetson-dashboard.service`.
- Startup at boot: the user unit is enabled and `loginctl show-user shai -p Linger` reports `yes`.
- Build tools: checksum-verified Go 1.27.3 under `~/.local/share/jetson-dashboard/toolchains/go1.27.3/go`. Commands are available at `~/.local/bin/go` and `~/.local/bin/gofmt`. Existing Python, Node, JetPack and OS packages were preserved.

The private-network HTTPS certificate is self-signed. Trust `/home/shai/.config/jetson-dashboard/cert.pem` on each client before ordinary use. Copy it over SSH or a trusted local channel and check its SHA-256 fingerprint locally:

```bash
openssl x509 -in ~/.config/jetson-dashboard/cert.pem -noout -fingerprint -sha256 -dates
```

Import that certificate in the client browser/OS trust store. The certificate covers the current IP, the device hostname and localhost; a hostname also needs to resolve to this device. A browser certificate warning indicates that trust has not been configured or the address/certificate does not match. The service refuses non-loopback plaintext HTTP. Do not forward this port to the public internet.

## Use

Click a panel heading or its arrow to collapse or expand it. All four metric cards plus System details, Storage, Network and Device files can collapse independently. Keyboard users can focus a heading and press Enter or Space. Panels start expanded when the page loads; collapsing them keeps live readings updating and preserves the current file-browser state.

Readings refresh every 2 seconds while the tab is visible. CPU and network rates need two samples after startup. Missing sensors show “Unavailable”. Failed requests preserve the last values and mark them stale. RAM is shared CPU/GPU system memory; it is not dedicated GPU memory. Disk availability excludes reserved blocks. Network rates are per interface.

Use folder names and breadcrumbs to navigate; downloads are streamed through ordinary browser links. Hidden files and folders (names beginning with a dot) are hidden by default. Tick **Show hidden files** beside Refresh to display them; untick it to hide them again. The checkbox applies as you navigate folders and resets when the page reloads. The item count reflects the visible entries. Hiding an entry only filters the listing; it does not change access permissions. The page can create files and folders, edit existing text files and delete entries; it cannot upload or execute files. Relative symlinks are allowed only within the shared tree. Lists stop at 1,000 entries and explain when a folder is truncated. At most two downloads run concurrently. A file being modified while downloaded is best-effort, not a snapshot.

### Create and delete files and folders

Use **New file** or **New folder** to create an entry in the current folder. Enter a single name; spaces, Unicode and dot-prefixed names are supported. Existing entries are never replaced. New files start empty and open immediately in the text editor. New files/folders use private permissions (0600/0700). Hidden creations remain hidden when **Show hidden files** is unchecked; the success message explains how to display them.

Use **Delete** on an entry to open its confirmation. File deletion needs an explicit confirmation; folder deletion requires typing the exact folder name and removes **everything inside it, including hidden files and subfolders**. Deletion is permanent and has no undo or recycle bin. Cancelling the dialog leaves the entry intact. Deleting a symlink removes the link itself and keeps its target. The root and parent navigation row have no Delete action. Changed confirmation targets are refused; changes made by unrelated processes during the final filesystem operation remain best-effort, as with editing. If a request times out, refresh before retrying because the operation may still finish.

Creation/deletion requires write/search permission on the containing folder. The supplied dedicated-account system unit keeps its filesystem restrictions; the current user service supports these operations where the owner has permission.

### Edit files

Click **Edit** beside a file to open a plain text editor. Change the text and click **Save**, or **Cancel** to close. Unsaved changes prompt before closing the editor or leaving the page. Save errors keep your text in the editor. If the file changed after you opened it, saving is refused: copy any edits you want to keep, close and reopen the file, then apply them to the latest version.

The browser editor supports existing UTF-8 text files up to **1 MiB**, including empty files. It rejects binary data, symbolic links, hard links and special files. It preserves owner/group, ordinary read/write/execute permission bits, and CRLF line endings in files that consistently use CRLF. Special mode flags (setuid/setgid/sticky), extended ACLs and other extended attributes are not preserved; use an external editor when these matter. Saving requires write access to both the file and its containing directory. Saves use a temporary file and atomic replacement; saves from this dashboard are serialized. External programs do not share its lock, so conflict detection for an external write during the final check/rename is best-effort. The supplied dedicated-account system unit retains its read-only filesystem restrictions; editing is available in the current user-service installation.

Click **External edit** for the file's full remote path, SFTP connection details and a copyable link. **Open SFTP app** hands the link to a registered SFTP application on your computer; if none is registered, configure your editor/file-transfer app manually:

- Protocol: **SFTP**
- Host: **192.168.68.107**, port **22**
- SSH username: **shai**
- Login: your existing device SSH key or account password, **separate from the dashboard password**

Open the displayed remote path in that app and save normally. The Jetson's SSH service and SFTP subsystem are already enabled. Desktop-app setup and login must be completed on your own computer. [Current VS Code Remote SSH](https://code.visualstudio.com/docs/remote/linux) requires newer Linux libraries than this Nano provides; an SFTP-capable editor avoids needing to run VS Code Server here.

```bash
# Read credentials privately on the device.
cat ~/.config/jetson-dashboard/credentials.txt

# Manage the installed service.
systemctl --user restart jetson-dashboard.service
systemctl --user stop jetson-dashboard.service
systemctl --user status jetson-dashboard.service
systemctl --user show jetson-dashboard.service -p MainPID -p ActiveState -p SubState
journalctl --user -u jetson-dashboard.service --no-pager -n 50
```

Journald availability depends on this OS's journal storage configuration; `systemctl --user status` also shows recent messages. No credentials or request query strings are logged.

## Build and test

Use a supported, patched Go release with Go 1.25+ APIs. On this device:

```bash
export PATH="$HOME/.local/bin:$PATH"
go test ./...
go vet ./...
python3 -m unittest discover -s deploy -p 'test_*.py'
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 go build -trimpath -o build/jetson-dashboard .
```

On a new development machine, download Go for its OS/architecture from [the official downloads](https://go.dev/dl/) and verify the published SHA-256 before extracting. No `go get`, `npm install` or Python packages are needed. The ARM64 binary can be cross-compiled on Linux. Tests use Linux filesystem interfaces, so run them on Linux.

```bash
# Install or update as the current user; requires Python 3.6+, OpenSSL and systemd.
python3 deploy/install.py --user --ip 192.168.68.107
```

The installer preserves credentials and certificates on repeat runs, replaces the binary atomically, and restarts the service. It requires a dedicated export directory and defaults to `/home/shai/dashboard-share`; after reinstalling, restore the current home-directory share by setting `DASHBOARD_FILE_ROOT="/home/shai"` in `~/.config/jetson-dashboard/dashboard.env` and restarting the user service. It enables linger when the OS allows it; otherwise an administrator can run `sudo loginctl enable-linger shai`. User mode runs as the device owner. Rooted file operations and the HTTP API restrict access to the configured file root; browser saves require filesystem write permission; user mode does not provide a separate OS account or filesystem namespace isolation.

For stronger OS isolation, the included system installer runs the same binary as a dedicated account:

```bash
# Stop the user service to release its port before switching installation mode.
systemctl --user disable --now jetson-dashboard.service
sudo python3 deploy/install.py --system --ip 192.168.68.107
sudo systemctl status jetson-dashboard.service
sudo systemd-analyze verify /etc/systemd/system/jetson-dashboard.service
```

System mode installs under `/opt/jetson-dashboard`, stores config/TLS under `/etc/jetson-dashboard`, and enables `NoNewPrivileges`, `PrivateTmp`, `PrivateDevices`, `ProtectSystem=strict` and `ProtectHome=read-only`. Its private credentials file is root-readable. The installer gives the export directory to the invoking owner with the service group and mode 2750, leaving existing content untouched. Existing files/subfolders also need group read/search access; grant access only to intended shared data:

```bash
sudo chgrp -R jetson-dashboard /home/shai/dashboard-share
sudo find /home/shai/dashboard-share -type d -exec chmod 2750 {} +
sudo find /home/shai/dashboard-share -type f -exec chmod 0640 {} +
sudo -u jetson-dashboard test -r /home/shai/dashboard-share
sudo -u jetson-dashboard test ! -w /home/shai/dashboard-share
```

Use group-readable permissions and `umask 0027` when creating new shared files. Applications using mode 0600 or moving files from elsewhere may need explicit group/permission changes. Every ancestor directory needs search permission for the service account. Do not bind-mount sensitive directories or place sensitive hard links into the export: rooted path APIs do not distinguish those objects.

## Remote access through ngrok

The installed user tunnel service exposes the dashboard at **https://mop-unsoiled-underdog.ngrok-free.dev**. Sign in using the current private dashboard credentials. It forwards to `https://192.168.68.107:8443`, verifies the dashboard certificate using the local certificate file, and disables ngrok request inspection. No router port forwarding or root privileges are required for this user service. It is enabled for user-service startup; the device already has linger enabled.

```bash
systemctl --user status ngrok-dashboard.service
systemctl --user restart ngrok-dashboard.service
systemctl --user stop ngrok-dashboard.service
```

The unit is `~/.config/systemd/user/ngrok-dashboard.service`; the account token remains in the protected `~/.config/ngrok/ngrok.yml`. The service owns the tunnel, so do not start another agent for the same URL while it is running. A bare `ngrok http 8443` targets plain HTTP on localhost and does not match this dashboard's LAN-only HTTPS listener. If manually launching after stopping the service, use:

```bash
ngrok http https://192.168.68.107:8443 \
  --url https://mop-unsoiled-underdog.ngrok-free.dev \
  --upstream-tls-verify \
  --upstream-tls-verify-cas ~/.config/jetson-dashboard/cert.pem \
  --inspect=false
```

The ngrok HTTPS tunnel carries browser/API traffic. External desktop SFTP editing still needs a separate route to the device's SSH server; the web tunnel does not forward SSH.

## Configuration, certificates and updates

`deploy/dashboard.env.example` lists the settings. User mode reads `~/.config/jetson-dashboard/dashboard.env`; system mode reads `/etc/jetson-dashboard/dashboard.env`.

| Variable | Meaning |
| --- | --- |
| `DASHBOARD_LISTEN` | Explicit numeric IP and port; default `127.0.0.1:8080` |
| `DASHBOARD_FILE_ROOT` | Dedicated export; default `/home/shai/dashboard-share`; never `/` |
| `DASHBOARD_USERNAME` | Sign-in username; defaults to `dashboard` |
| `DASHBOARD_SECRET_SHA256` | Required 64-character SHA-256 hex digest of a generated 32-byte random secret |
| `DASHBOARD_TLS_CERT`, `DASHBOARD_TLS_KEY` | Both PEM paths required for LAN access |

Basic authentication uses the browser's native sign-in dialog. At startup the service reads `DASHBOARD_USERNAME` and `DASHBOARD_SECRET_SHA256` from `dashboard.env`; editing `credentials.txt` alone does not update these active settings. When changing credentials, synchronize the username and SHA-256 password digest into those settings, then restart the service. Browser credential caching controls sign-out; use a private window or close the browser session. This scheme is designed for a generated high-entropy secret, not a human-chosen password. To rotate it, generate 32 random bytes with Python `secrets.token_urlsafe(32)`, update the private credentials file and its SHA-256 digest in `dashboard.env`, then restart. Keep both files mode 0600. Never put either file in the export.

Certificates last 365 days. Renew before their `notAfter` date by rerunning the installer with `--renew-cert`, then trust the replacement on clients. An expired, future-dated or mismatched certificate fails startup clearly. Set a DHCP reservation for `192.168.68.107`; changing the address requires updating the listener and certificate. For a different IP, rerun the installer with `--ip <new-private-IP> --renew-cert`.

Before updating, keep a copy of the previous installed binary outside the export. Build, test and rerun the installer. For rollback, stop the service, restore that binary, then start it. Shared files and credentials are independent of binary replacement.

For loopback use through SSH, set `DASHBOARD_LISTEN=127.0.0.1:8080`, remove both TLS settings, restart, then forward `ssh -L 8080:127.0.0.1:8080 shai@192.168.68.107` and open `http://127.0.0.1:8080` on the client. The password travels inside the SSH tunnel. Remote use should go through an existing VPN or SSH forwarding.

## Verification and limits

Go tests cover auth and listener/certificate validation; fixture-based telemetry and counter resets; rooted paths, encoding, symlinks and replacement races; special-file rejection; listing limits; byte ranges/HEAD; Unicode attachment names; permission errors and concurrent download slots.

Optional real browser checks use the existing Chromium and Python standard library:

```bash
chromium-browser --headless --disable-gpu --ignore-certificate-errors \
  --remote-debugging-address=127.0.0.1 --remote-debugging-port=19223 \
  --user-data-dir=/tmp/jetson-dashboard-browser-check about:blank
# In another terminal, from this project:
python3 scripts/browser_check.py
python3 scripts/editor_check.py
python3 scripts/manage_check.py
python3 scripts/collapse_check.py
python3 scripts/device_check.py --seconds 300
```

Use a disposable browser profile, keep the debug port on loopback, and stop that Chromium instance when finished. `--ignore-certificate-errors` applies only to this disposable test browser; ordinary clients should trust the certificate. The scripts temporarily create their own test entries under the export and remove them afterward. Results/screenshots go under ignored `build/`.

The Nano's 39-bit virtual address layout is unsupported by Go's ThreadSanitizer: `go test -race ./...` exits with `unsupported VMA range (Found 39 - Supported 48)`. Run that extra check on a supported Linux development host. Ordinary tests and the production binary run on the Nano. `systemd-analyze verify` under this non-root systemd 237 session also cannot create its temporary manager cgroup; actual service load/start and configuration validation are checked instead.

See [acceptance results](docs/acceptance.md) for measured results. Physical access from a second device, client trust installation, a full external-internet disconnection test and a reboot remain operator checks; this session does not reboot the Nano. The user service is enabled with linger, and restart is tested without rebooting. Resource targets are under 100 MiB RSS and 5% of one CPU core with one active polling client, excluding downloads.

## Uninstall

```bash
systemctl --user disable --now jetson-dashboard.service
rm ~/.config/systemd/user/jetson-dashboard.service
systemctl --user daemon-reload
```

Then remove the installed binary under `~/.local/share/jetson-dashboard/jetson-dashboard` if desired. Keep `/home/shai/dashboard-share` and `~/.config/jetson-dashboard` unless deliberately removing shared data/credentials. The toolchains are separate and may be retained. Disabling linger affects other user services too; leave it enabled if they use it. For a system install, disable the system service and remove its unit/binary; preserve the export and review whether the account/config are still needed.
