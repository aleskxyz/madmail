#!/bin/bash
# ────────────────────────────────────────────────────────────────
# build-legacy-lxc.sh
# Build and verify the legacy (static) madmail binary inside an
# Ubuntu 22.04 LXC container to guarantee GLIBC independence.
#
# Usage:
#   ./scripts/build-legacy-lxc.sh              # Build + verify
#   ./scripts/build-legacy-lxc.sh --verify     # Only verify existing binary
#   ./scripts/build-legacy-lxc.sh --clean      # Destroy the build container
# ────────────────────────────────────────────────────────────────
set -e

CONTAINER="madmail-legacy-build"
DISTRO="ubuntu"
RELEASE="jammy"
ARCH="amd64"
LEGACY_BINARY="build/maddy-amd64-legacy"
GO_VERSION="1.24.0"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { echo -e "${CYAN}[legacy-build]${NC} $*"; }
ok()  { echo -e "${GREEN}✅ $*${NC}"; }
warn() { echo -e "${YELLOW}⚠️  $*${NC}"; }
err() { echo -e "${RED}❌ $*${NC}"; }

# ──── Parse args ────
MODE="build"
for arg in "$@"; do
    case "$arg" in
        --verify) MODE="verify" ;;
        --clean)  MODE="clean" ;;
    esac
done

# ──── Clean mode ────
if [ "$MODE" = "clean" ]; then
    log "Destroying container $CONTAINER..."
    sudo lxc-stop -n "$CONTAINER" -k 2>/dev/null || true
    sudo lxc-destroy -n "$CONTAINER" 2>/dev/null || true
    ok "Container $CONTAINER destroyed."
    exit 0
fi

# ──── Verify mode: just test existing binary in LXC ────
verify_in_lxc() {
    if [ ! -f "$LEGACY_BINARY" ]; then
        err "Legacy binary not found at $LEGACY_BINARY"
        err "Build it first: make build_legacy"
        exit 1
    fi

    log "Verifying binary linkage..."
    FILE_INFO=$(file "$LEGACY_BINARY")
    echo "  $FILE_INFO"

    if echo "$FILE_INFO" | grep -q "statically linked"; then
        ok "Binary is statically linked"
    else
        err "Binary is NOT statically linked!"
        ldd "$LEGACY_BINARY" 2>&1 || true
        exit 1
    fi

    # Check if container exists, create if not
    EXISTING=$(sudo lxc-ls 2>/dev/null || true)
    if ! echo "$EXISTING" | grep -qw "$CONTAINER"; then
        log "Creating Ubuntu 22.04 verification container..."
        sudo lxc-create -n "$CONTAINER" -t download -- -d "$DISTRO" -r "$RELEASE" -a "$ARCH"
    fi

    # Ensure it's running
    INFO=$(sudo lxc-info -n "$CONTAINER" 2>/dev/null || true)
    if echo "$INFO" | grep -q "STOPPED"; then
        log "Starting container..."
        sudo lxc-start -n "$CONTAINER"
        sleep 3
    fi

    # Push binary into container
    log "Pushing binary into Ubuntu 22.04 container..."
    cat "$LEGACY_BINARY" | sudo lxc-attach -n "$CONTAINER" -- sh -c "cat > /tmp/madmail && chmod +x /tmp/madmail"

    # Run version check
    log "Running version check inside Ubuntu 22.04..."
    sudo lxc-attach -n "$CONTAINER" -- /tmp/madmail version && ok "Binary runs on Ubuntu 22.04!" || {
        err "Binary FAILED to run on Ubuntu 22.04!"
        sudo lxc-attach -n "$CONTAINER" -- cat /etc/os-release | head -3
        exit 1
    }

    # Show container OS info
    log "Container OS:"
    sudo lxc-attach -n "$CONTAINER" -- cat /etc/os-release | grep -E "^(PRETTY_NAME|VERSION)" | head -2

    # Show GLIBC version in container
    log "Container GLIBC version:"
    sudo lxc-attach -n "$CONTAINER" -- ldd --version 2>&1 | head -1 || true

    ok "Verification complete: binary is GLIBC-independent and runs on Ubuntu 22.04"
}

# ──── Build mode: build locally + verify in LXC ────
if [ "$MODE" = "verify" ]; then
    verify_in_lxc
    exit 0
fi

# Full build + verify
log "Building legacy static binary..."
make build_legacy

log "Build complete. Binary details:"
ls -lh "$LEGACY_BINARY"
file "$LEGACY_BINARY"

# Verify
verify_in_lxc

ok "Legacy build pipeline complete!"
echo ""
echo "  Binary:  $LEGACY_BINARY"
echo "  Type:    Statically linked (no GLIBC dependency)"
echo "  Compat:  Ubuntu 20.04+, Debian 10+, any Linux x86_64"
echo ""
