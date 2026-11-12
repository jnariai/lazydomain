#!/usr/bin/env bash
set -uo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
SH=${TEST_SH:-sh}
work=$(mktemp -d)
server_pid=""
cleanup() {
  [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT

passed=0
failed=0
www="$work/www"
releases="$www/jnariai/lazydomain/releases"
mkdir -p "$releases" "$work/shim" "$work/home"


make_release() {
  local tag=$1
  shift
  local version=${tag#v} dir="$releases/download/$tag" arch stage
  [ $# -gt 0 ] || set -- amd64 arm64
  mkdir -p "$dir"
  for arch in "$@"; do
    stage=$(mktemp -d -p "$work")
    cat >"$stage/lazydomain" <<EOF
#!/bin/sh
case "\$1" in
  version) echo "$version ($arch)" ;;
  uninstall) shift; echo "uninstall \$*" >>"\${LD_STUB_LOG:-/dev/null}" ;;
esac
EOF
    chmod +x "$stage/lazydomain"
    tar -czf "$dir/lazydomain_${version}_linux_${arch}.tar.gz" -C "$stage" lazydomain
  done
  (cd "$dir" && sha256sum -- *.tar.gz >checksums.txt)
}

cat >"$work/shim/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) echo "${FAKE_OS:-Linux}" ;;
  -m) echo "${FAKE_ARCH:-x86_64}" ;;
  *) /usr/bin/uname "$@" ;;
esac
EOF
chmod +x "$work/shim/uname"

cat >"$work/server.py" <<'EOF'
import functools, http.server, sys
www, latest_file, port_file = sys.argv[1:4]
base = "/jnariai/lazydomain/releases"

class Handler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def route(self):
        path = self.path.split("?")[0].rstrip("/")
        if path == base + "/latest":
            tag = open(latest_file).read().strip()
            self.send_response(302)
            self.send_header("Location", base + ("/tag/" + tag if tag else ""))
            self.send_header("Content-Length", "0")
            self.end_headers()
            return True
        if path == base or path.startswith(base + "/tag/"):
            self.send_response(200)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return True
        return False

    def do_HEAD(self):
        if not self.route():
            super().do_HEAD()

    def do_GET(self):
        if not self.route():
            super().do_GET()

server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Handler, directory=www))
open(port_file, "w").write(str(server.server_address[1]))
server.serve_forever()
EOF

: >"$work/latest"
python3 -I "$work/server.py" "$www" "$work/latest" "$work/port" &
server_pid=$!
for _ in $(seq 50); do [ -s "$work/port" ] && break; sleep 0.1; done
[ -s "$work/port" ] || { echo "fake GitHub server did not start"; exit 1; }
github="http://127.0.0.1:$(cat "$work/port")"

make_release v1.1.0
make_release v1.2.0


base_path="$work/shim:/usr/local/bin:/usr/bin:/bin"

# run_install <dest> [install.sh args...]; extra env comes from the caller.
# Sets $rc, returns it, and writes combined output to $out.
out="$work/out"
run_install() {
  local dest=$1
  shift
  env HOME="$work/home" PATH="${TEST_PATH:-$base_path}" \
    LAZYDOMAIN_GITHUB_URL="$github" LAZYDOMAIN_INSTALL_DIR="$dest" \
    "$SH" "$repo/install.sh" "$@" >"$out" 2>&1 </dev/null
  rc=$?
  return "$rc"
}

newdest() { mktemp -d -p "$work"; }

check() {
  local name=$1
  shift
  if "$@"; then
    passed=$((passed + 1))
    printf '  ok    %s\n' "$name"
  else
    failed=$((failed + 1))
    printf '  FAIL  %s\n' "$name"
    sed 's/^/        | /' "$out"
  fi
}

ok() { [ "$rc" -eq 0 ]; }
nok() { [ "$rc" -ne 0 ]; }
says() { grep -qF -- "$1" "$out"; }
installed_version() { [ "$("$1/lazydomain" version)" = "$2" ]; }
not_installed() { [ ! -e "$1/lazydomain" ]; }


echo "install.sh under $SH"

t_latest() {
  echo v1.2.0 >"$work/latest"
  local d
  d=$(newdest)
  run_install "$d"
  ok && installed_version "$d" "1.2.0 (amd64)" && [ -x "$d/lazydomain" ] &&
    says "Installed 1.2.0 (amd64)" && says "lazydomain proxy up"
}
check "installs the latest release" t_latest

t_path_warning() {
  local d
  d=$(newdest)
  run_install "$d"
  ok && says "is not on your PATH"
}
check "warns when the install dir is not on PATH" t_path_warning

t_no_path_warning() {
  local d
  d=$(newdest)
  TEST_PATH="$d:$base_path" run_install "$d"
  ok && ! says "is not on your PATH"
}
check "no PATH warning when the install dir is on PATH" t_no_path_warning

t_pinned() {
  local d
  d=$(newdest)
  LAZYDOMAIN_VERSION=1.1.0 run_install "$d"
  ok && installed_version "$d" "1.1.0 (amd64)"
}
check "LAZYDOMAIN_VERSION pins a release (with or without v)" t_pinned

t_upgrade() {
  local d
  d=$(newdest)
  LAZYDOMAIN_VERSION=v1.1.0 run_install "$d" && installed_version "$d" "1.1.0 (amd64)" || return 1
  echo v1.2.0 >"$work/latest"
  run_install "$d"
  ok && installed_version "$d" "1.2.0 (amd64)"
}
check "re-running upgrades in place" t_upgrade

t_creates_dir() {
  local d
  d="$(newdest)/does/not/exist"
  run_install "$d"
  ok && installed_version "$d" "1.2.0 (amd64)"
}
check "creates a missing install dir" t_creates_dir

t_arm64() {
  local d
  d=$(newdest)
  FAKE_ARCH=aarch64 run_install "$d"
  ok && installed_version "$d" "1.2.0 (arm64)"
}
check "picks the arm64 build on aarch64" t_arm64

t_bad_os() {
  local d
  d=$(newdest)
  FAKE_OS=Darwin run_install "$d"
  nok && says "Linux and WSL2 only" && not_installed "$d"
}
check "rejects non-Linux systems" t_bad_os

t_bad_arch() {
  local d
  d=$(newdest)
  FAKE_ARCH=riscv64 run_install "$d"
  nok && says "unsupported CPU architecture: riscv64" && not_installed "$d"
}
check "rejects unsupported CPUs" t_bad_arch

t_checksum_mismatch() {
  make_release v9.0.0 amd64
  sed -i -E 's/^[0-9a-f]{8}/00000000/' "$releases/download/v9.0.0/checksums.txt"
  local d
  d=$(newdest)
  LAZYDOMAIN_VERSION=v9.0.0 run_install "$d"
  nok && says "checksum mismatch" && not_installed "$d"
}
check "refuses an archive whose checksum does not match" t_checksum_mismatch

t_checksum_missing() {
  make_release v9.1.0 amd64
  : >"$releases/download/v9.1.0/checksums.txt"
  local d
  d=$(newdest)
  LAZYDOMAIN_VERSION=v9.1.0 run_install "$d"
  nok && says "is not listed in checksums.txt" && not_installed "$d"
}
check "refuses an archive missing from checksums.txt" t_checksum_missing

t_upgrade_failure_keeps_old() {
  local d
  d=$(newdest)
  LAZYDOMAIN_VERSION=v1.1.0 run_install "$d" || return 1
  LAZYDOMAIN_VERSION=v9.0.0 run_install "$d"
  nok && installed_version "$d" "1.1.0 (amd64)"
}
check "a failed upgrade leaves the installed version alone" t_upgrade_failure_keeps_old

t_unknown_release() {
  local d
  d=$(newdest)
  LAZYDOMAIN_VERSION=v404.0.0 run_install "$d"
  nok && says "does release v404.0.0 exist?" && not_installed "$d"
}
check "explains a release that does not exist" t_unknown_release

t_no_releases() {
  : >"$work/latest"
  local d
  d=$(newdest)
  run_install "$d"
  echo v1.2.0 >"$work/latest"
  nok && says "no published release" && not_installed "$d"
}
check "explains a repository without releases" t_no_releases

t_unknown_option() {
  local d
  d=$(newdest)
  run_install "$d" --bogus
  nok && says "unknown option '--bogus'"
}
check "rejects unknown options" t_unknown_option

t_uninstall_delegates() {
  local d log
  d=$(newdest)
  log="$work/uninstall.log"
  : >"$log"
  run_install "$d" || return 1
  LD_STUB_LOG="$log" run_install "$d" --uninstall --yes
  ok && [ "$(cat "$log")" = "uninstall --yes" ]
}
check "--uninstall runs 'lazydomain uninstall' with the extra arguments" t_uninstall_delegates

t_uninstall_from_path() {
  local d other log
  d=$(newdest)
  other=$(newdest)
  log="$work/uninstall-path.log"
  : >"$log"
  run_install "$other" || return 1
  LD_STUB_LOG="$log" TEST_PATH="$other:$base_path" run_install "$d" --uninstall
  ok && [ "$(cat "$log")" = "uninstall " ]
}
check "--uninstall falls back to lazydomain on PATH" t_uninstall_from_path

t_uninstall_missing() {
  local d
  d=$(newdest)
  run_install "$d" --uninstall
  nok && says "lazydomain is not installed"
}
check "--uninstall explains when lazydomain is not installed" t_uninstall_missing

echo
echo "$passed passed, $failed failed"
[ "$failed" -eq 0 ]
