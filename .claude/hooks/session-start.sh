#!/bin/bash
# Cloud-only: make the Docker-based dev stack (`moon frontend:dev`) usable.
# Idempotent; safe to run on every session start/resume.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}"

# dev.env is gitignored; compose reads it for substitution. Blank the ngrok
# token so the optional ngrok container doesn't try to authenticate.
if [ ! -f dev.env ] && [ -f dev.env.example ]; then
  cp dev.env.example dev.env
  sed -i 's/^NGROK_AUTHTOKEN=.*/NGROK_AUTHTOKEN=/' dev.env
fi

# Expose the agent proxy CA as a build context dir for Dockerfiles that accept
# an optional `extra-ca` context (EXTRA_CA_DIR in docker-compose.yml). Containers
# don't trust the proxy's CA on their own, so image builds fail without it.
if [ -f /root/.ccr/agent-proxy-ca.crt ]; then
  mkdir -p /opt/extra-ca
  cp /root/.ccr/agent-proxy-ca.crt /opt/extra-ca/agent-proxy-ca.crt
  if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
    echo 'export EXTRA_CA_DIR=/opt/extra-ca' >> "$CLAUDE_ENV_FILE"
  fi
fi

# dockerd is installed but not running in cloud sessions; background processes
# don't survive between sessions, so start it every time.
if command -v dockerd >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
  setsid nohup dockerd >/var/log/dockerd.log 2>&1 < /dev/null &
  for _ in $(seq 1 60); do
    docker info >/dev/null 2>&1 && break
    sleep 1
  done
  docker info >/dev/null 2>&1 || echo "warning: dockerd did not become ready (see /var/log/dockerd.log)" >&2
fi
