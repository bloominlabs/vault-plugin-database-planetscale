#!/usr/bin/env bash
set -eEuo pipefail

MNT_PATH="planetscale"
PLUGIN_NAME="vault-plugin-database-planetscale"

#
# Helper script for local development. Automatically builds and registers the
# plugin. Requires `vault` is installed and available on $PATH.
#

# Get the right dir
DIR="$(cd "$(dirname "$(readlink "$0")")" && pwd)"

echo "==> Starting dev"

echo "--> Scratch dir"
echo "    Creating"
SCRATCH="${DIR}/tmp"
mkdir -p "${SCRATCH}/plugins"

function cleanup {
  echo ""
  echo "==> Cleaning up"
  kill -INT "${VAULT_PID}"
  rm -rf "${SCRATCH}"
}
trap cleanup EXIT

echo "--> Starting server"

export VAULT_TOKEN="root"
export VAULT_ADDR="http://127.0.0.1:8200"

vault server \
  -dev \
  -dev-plugin-init \
  -dev-plugin-dir "./bin/" \
  -dev-root-token-id "root" \
  -log-level "debug" \
  &
sleep 2
VAULT_PID=$!

# vault secrets enable -path=${MNT_PATH} -plugin-name=${PLUGIN_NAME} plugin

SHASUM=$(sha256sum ./bin/$PLUGIN_NAME | cut -d " " -f1)

echo $SHASUM

echo "    Mouting plugin"
vault secrets enable database
vault write sys/plugins/catalog/database/$PLUGIN_NAME \
  sha256=$SHASUM \
  command="$PLUGIN_NAME"

vault write database/config/$MNT_PATH \
    plugin_name=$PLUGIN_NAME \
    allowed_roles="admin" \
    organization="bloominlabs" \
    database="bloominlabs" \
    service_token=$PLANETSCALE_SERVICE_TOKEN \
    service_token_id=$PLANETSCALE_SERVICE_TOKEN_ID 

# vault read database/creds/admin
vault write database/roles/admin \
    db_name=$MNT_PATH \
    creation_statements='{"branch": "main"}' \
    default_ttl="1h" \
    max_ttl="24h"

# TODO: set the service token for the endpoint
echo "==> Ready!"
wait ${VAULT_PID}
