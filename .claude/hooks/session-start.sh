#!/bin/bash
# Cloud-only: dockerd is installed but not running in cloud sessions, and
# background processes don't survive between sessions, so start it each time.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

if command -v dockerd >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
  setsid nohup dockerd >/var/log/dockerd.log 2>&1 < /dev/null &
  for _ in $(seq 1 60); do
    docker info >/dev/null 2>&1 && break
    sleep 1
  done
  docker info >/dev/null 2>&1 || echo "warning: dockerd did not become ready (see /var/log/dockerd.log)" >&2
fi
