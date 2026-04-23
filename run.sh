#!/usr/bin/env bash
set -euo pipefail

BINARY="$HOME/bin/mcp-indexer"
CMD_PKG="./cmd/..."

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [args]

Commands:
  build              Build and install to $BINARY
  ui <serviceId>     Start interactive graph viz (default port 8080)
    --port <N>       Override HTTP port
  help               Show this help
EOF
}

cmd="${1:-help}"
shift || true

case "$cmd" in
  build)
    echo "Building..."
    go build -o "$BINARY" $CMD_PKG
    echo "Installed: $BINARY"
    ;;

  ui)
    if [[ $# -eq 0 ]]; then
      echo "Error: serviceId required" >&2
      echo "" >&2
      SERVICES=$("$BINARY" list 2>/dev/null || true)
      if [[ -n "$SERVICES" ]]; then
        echo "Registered services:" >&2
        echo "$SERVICES" | awk '{print "  "$1}' >&2
      else
        echo "(no services registered)" >&2
      fi
      echo "" >&2
      echo "Usage: $(basename "$0") ui <serviceId> [--port N]" >&2
      exit 1
    fi
    SERVICE="$1"; shift

    if ! "$BINARY" list 2>/dev/null | awk '{print $1}' | grep -qx "$SERVICE"; then
      echo "Error: service \"$SERVICE\" not found" >&2
      echo "" >&2
      SERVICES=$("$BINARY" list 2>/dev/null || true)
      if [[ -n "$SERVICES" ]]; then
        echo "Registered services:" >&2
        echo "$SERVICES" | awk '{print "  "$1}' >&2
      else
        echo "(no services registered)" >&2
      fi
      exit 1
    fi

    exec "$BINARY" viz "$SERVICE" "$@"
    ;;

  help|--help|-h)
    usage
    ;;

  *)
    echo "Unknown command: $cmd" >&2
    usage >&2
    exit 1
    ;;
esac
