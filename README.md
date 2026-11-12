# lazydomain

Trusted `https://<project>.test` domains for Dockerized Laravel apps (Sail and
similar) running inside WSL2, with the browser on Windows.

> Status: MVP. TUI plus `list`, `add`, `remove`, `doctor`, `proxy up/down`
> and `trust` commands.

## How it works

```
Windows browser ──► my-app.test → 127.0.0.1   (Windows hosts file, managed block)
                ──► 127.0.0.1:443             (WSL localhost forwarding, NAT mode)
                ──► lazydomain-caddy          (Docker, network_mode: host, TLS from its local CA)
                ──► 127.0.0.1:<APP_PORT>      (port your Sail app publishes)
```

| Piece | Where | Notes |
|---|---|---|
| Proxy | `lazydomain-caddy` container | Config is pushed via the admin API on `localhost:2019`; `--resume` keeps routes across reboots. |
| CA | `/data` volume of the proxy | Trusted once in Windows (`Cert:\CurrentUser\Root`) and in WSL. Deleting the volume creates a new CA. |
| DNS | Windows hosts file | Only the block between the `# BEGIN lazydomain` and `# END lazydomain` markers is touched. |
| Names | Project directory name | Normalized to a DNS label: `My_App` → `my-app.test`, plus `vite.my-app.test`. |

## Requirements (MVP)

- WSL2 on Windows 11 with `systemd=true` in `/etc/wsl.conf`. Both networking
  modes work: in `nat` Windows reaches the proxy through localhost forwarding,
  in `mirrored` Windows and WSL share `127.0.0.1`.
- Docker Engine and the compose plugin inside the distro
- Laravel 11+ projects under `~/Projects`

## Install

Run inside your WSL distro (Linux amd64 or arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/jnariai/lazydomain/main/install.sh | sh
```

The script downloads the latest release, verifies it against the release's
`checksums.txt`, and installs `lazydomain` to `~/.local/bin`. It never uses
sudo. Run the same command again to upgrade. Options:

```bash
curl -fsSL https://raw.githubusercontent.com/jnariai/lazydomain/main/install.sh | LAZYDOMAIN_VERSION=v0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/jnariai/lazydomain/main/install.sh | LAZYDOMAIN_INSTALL_DIR=/usr/local/bin sh
```

Other ways to install:

```bash
go install github.com/jnariai/lazydomain/cmd/lazydomain@latest   # Go 1.24+
make install                                                     # from a clone, to ~/.local/bin
```

### Uninstall

```bash
lazydomain uninstall
```

or, with the same one-liner used to install:

```bash
curl -fsSL https://raw.githubusercontent.com/jnariai/lazydomain/main/install.sh | sh -s -- --uninstall
```

After you type `yes`, it removes, in this order:
1. every `*.test` entry it added to the Windows hosts file (one UAC prompt)
2. its root certificate from Windows (a confirmation dialog) and from WSL (sudo)
3. the proxy container and its volumes
4. `~/.config/lazydomain`
5. the binary itself

Your projects are never modified. If any of steps 1–3 fails, the config and
the binary are kept so you can run it again. Use `--yes` for non-interactive
use. (`make uninstall` only deletes the binary.)

### Using it

Then use it from any directory, for example inside a project:

```bash
cd ~/Projects/my-app && lazydomain add
```

## Usage

### TUI

```bash
lazydomain
```

Projects are listed on the left (● active, ● drifted, ○ available, ○ unsupported).
The right panel shows the selected project's domains, status and the project
changes that are still missing.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `a` | add the selected project (or re-apply a drifted one) |
| `x` | remove (asks for confirmation) |
| `o` | open `https://<name>.test` in the Windows browser |
| `D` | doctor checks |
| `u` / `t` | `proxy up` / `trust` (runs in the terminal, then returns) |
| `r` | refresh (it also refreshes every 5 seconds) |
| `q` | quit |

### CLI

```bash
lazydomain proxy up    # one-time: start the shared Caddy proxy, generates the local CA
lazydomain trust       # one-time: trust the CA (one Windows dialog + sudo in WSL)
lazydomain doctor      # verify every layer

lazydomain list                  # Laravel projects in ~/Projects and their status
lazydomain add my-app --dry-run  # show routes + hosts block, change nothing
lazydomain add my-app            # https://my-app.test + https://vite.my-app.test
lazydomain add                   # same, for the project in the current directory
lazydomain remove my-app
```

`add` updates the proxy and the Windows hosts file (one UAC prompt), then
prints only the project changes that are still missing (`APP_URL`,
`trustProxies`, the Vite `server` block). It never edits your project.
`APP_PORT` must not be 80 or 443, since the proxy owns those ports. Running
`add` again for an existing site re-applies it, for example after changing
`APP_PORT`.

State lives in `~/.config/lazydomain/state.json`. `list` compares it with the
proxy config, the hosts file and each project's `.env`, and flags any drift.

## Testing

```bash
make test           # Go unit tests (includes the uninstall flow with faked system steps)
make test-install   # install.sh against a local fake GitHub, under sh and bash
```

`tests/install_test.sh` covers installing the latest or a pinned release,
upgrading, checksum failures, OS/CPU detection and `--uninstall`. It only
touches a temp directory: stub binaries, a minimal `PATH` and a fake `HOME`.
CI runs both suites on every push and pull request.

## Releasing

Releases are built by [GoReleaser](https://goreleaser.com) in GitHub Actions
(`.github/workflows/release.yml`) whenever a `v*` tag is pushed:

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The release contains `lazydomain_<version>_linux_{amd64,arm64}.tar.gz` and
`checksums.txt`, which is exactly what `install.sh` expects. To build the
same artifacts locally into `dist/` without publishing, run
`goreleaser release --snapshot --clean`.

## Layout

```
cmd/lazydomain        CLI entry point
internal/tui          Bubble Tea interface
internal/sites        add/remove/list use cases shared by CLI and TUI
internal/doctor       read-only environment checks
internal/state        state.json: managed sites → proxy routes + hosts entries
internal/reconcile    apply state (proxy, then hosts, with rollback) and detect drift
internal/laravel      project discovery, composer.lock version, .env ports
internal/envfile      read-only .env parser
internal/proxy        Caddy compose file, JSON config builder, admin client
internal/docker       docker CLI wrapper
internal/platform     WSL/Linux detection, PowerShell interop
internal/hostsfile    managed-block parse/replace (CRLF-safe)
internal/names        directory name → DNS label
```
