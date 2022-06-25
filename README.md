# vault-plugin-database-planetscale

Generate @planetscale usernames and passwords using vault.

## Usage

### Setup Endpoint

1. Download and enable plugin locally

```bash
vault secrets enable database
vault write sys/plugins/catalog/database/vault-plugin-database-planetscale \
  sha256=<SHA256SUM of plugin> \
  command="vault-plugin-database-planetscale"
```

2. Configure a database the plugin

   ```bash
   # you can generate a service token withhttps://docs.planetscale.com/concepts/service-tokens
   vault write database/config/planetscale \
     plugin_name=vualt-plugin-database-planetscale \
     allowed_roles="admin" \
     organization="<your organization>" \
     database="<your database>" \
     service_token="<service_token>" \
     service_token_id="<service_token_id>"

   ```

3. Configure a role

   ```bash
   vault write database/roles/admin \
       db_name=$MNT_PATH \
       creation_statements='{"branch": "main", "role": "admin"}' \
       default_ttl="1h" \
       max_ttl="24h"
   ```

### Configure Role

Roles are have a configurable 'branch' and 'role' that you can specifying using the `creation_statements` parameter

```bash
vault write database/roles/admin \
    db_name=$MNT_PATH \
    creation_statements='{"branch": "main", "role": "admin"}' \
    default_ttl="1h" \
    max_ttl="24h"
```

### Rotating the Root Token

The is not currently implemented, but will be added in the future.

### Generate a new Token

To generate a new token:

[Configure a Role](#configure-role) and perform a 'read' operation on the `creds/<role-name>` endpoint.

```bash
# To read data using the api
$ vault read database/creds/admin
Key                Value
---                -----
lease_id           database/creds/admin/p2rG2nCorEVTUTVpXnb0NHsh
lease_duration     1h
lease_renewable    true
password           <password>
username           v-token-admin-qrez41hrdjt3n1zviwaz-1657678284
```

## Development

The provided [Earthfile] ([think makefile, but using
docker](https://earthly.dev)) is used to build, test, and publish the plugin.
See the build targets for more information. Common targets include

```bash
# build a local version of the plugin
$ earthly +build

# execute integration tests
#
# use https://developers.auth0.com/api/tokens/create to create a token
# with 'User:API Tokens:Edit' permissions
$ TEST_auth0_TOKEN=<YOUR_auth0_TOKEN> earthly --secret TEST_auth0_TOKEN +test

# start vault and enable the plugin locally
earthly +dev
```

[vault]: https://www.vaultproject.io/
[auth0-management-api-tokens]: https://auth0.com/docs/security/tokens/access-tokens/management-api-access-tokens
[earthfile]: ./Earthfile
[secrets plugin]: https://www.vaultproject.io/docs/secrets
[database plugin]: https://www.vaultproject.io/docs/secrets/databases
