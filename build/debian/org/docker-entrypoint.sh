#!/usr/bin/env bash
set -euo pipefail

SECRETS_FILE="/data/.secrets.env"

# Load previously generated secrets so the server stays stable across restarts
if [ -f "$SECRETS_FILE" ]; then
    # shellcheck source=/dev/null
    source "$SECRETS_FILE"
fi

# Auto-generate any missing required secrets and persist them to the volume.
# If you want to supply your own keys, set the env vars before starting the container
# and they will be used as-is (nothing will be written to the secrets file for those).
generate_secret() {
    local var_name="$1"
    if [ -z "${!var_name:-}" ]; then
        local value
        value=$(/app/keygen)
        export "$var_name=$value"
        echo "export $var_name=\"$value\"" >> "$SECRETS_FILE"
        echo "[org] generated $var_name and saved to $SECRETS_FILE"
    fi
}

generate_secret HABITAT_PDS_CRED_ENCRYPT_KEY
generate_secret HABITAT_OAUTH_SERVER_SECRET
generate_secret HABITAT_OAUTH_CLIENT_SECRET

exec /app/org "$@"
