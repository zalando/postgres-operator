<h1>Operator-generated PgBouncer config (Helm)</h1>

By default the connection pooler relies on the PgBouncer image's entrypoint to render `pgbouncer.ini` from environment variables (this is what the bundled `ghcr.io/zalando/postgres-operator/pgbouncer` image does). Some images — for example the **Chainguard FIPS PgBouncer** image — ship no such entrypoint.

When `connection_pooler_generate_config` is enabled, the operator renders the config itself instead of relying on the image. For every pooler it:

- renders `pgbouncer.ini` and stores it in a ConfigMap named `<pooler>-config` (e.g. `acid-minimal-cluster-pooler-config`);
- mounts that ConfigMap into the pooler container at `connection_pooler_config_path` using a `subPath`;
- overrides the container `command`/`args` (when set) so PgBouncer reads the mounted file;
- stamps the pod template with an `acid.zalan.do/pgbouncer-config-checksum` annotation, so the pooler restarts automatically when the rendered config changes.

The feature is **opt-in**; with the default `connection_pooler_generate_config: false` nothing changes for existing clusters.

## 1. Configure the operator via the Helm chart

These settings are operator-wide defaults and live under `configConnectionPooler` in the chart's `values.yaml`.

```yaml
configConnectionPooler:
  # Point the pooler at an image whose entrypoint does NOT render pgbouncer.ini.
  # Replace with your actual image reference.
  connection_pooler_image: "cgr.dev/chainguard/pgbouncer-fips:latest"

  # Let the operator render and own pgbouncer.ini.
  connection_pooler_generate_config: true

  # Optional: override the container entrypoint. Leave unset to keep the image's
  # own entrypoint. Set it when the image has no entrypoint that starts pgbouncer.
  connection_pooler_command:
    - "pgbouncer"
  # Args are applied only when generate_config is true. The default already points
  # pgbouncer at the mounted config file, so you usually don't need to change it.
  connection_pooler_args:
    - "/etc/pgbouncer/pgbouncer.ini"

  # Written into the generated pgbouncer.ini.
  connection_pooler_auth_type: "scram-sha-256"
  # Where the ConfigMap is mounted (and where args/command should point).
  connection_pooler_config_path: "/etc/pgbouncer/pgbouncer.ini"
```

Install or upgrade the operator with these values:

```bash
helm upgrade --install postgres-operator ./charts/postgres-operator \
  --namespace postgres-operator --create-namespace \
  -f values-pooler.yaml
```

Or set individual values inline:

```bash
helm upgrade --install postgres-operator ./charts/postgres-operator \
  --namespace postgres-operator --create-namespace \
  --set configConnectionPooler.connection_pooler_generate_config=true \
  --set configConnectionPooler.connection_pooler_image="cgr.dev/chainguard/pgbouncer-fips:latest"
```

| Value (under `configConnectionPooler`) | Default | Purpose |
|---|---|---|
| `connection_pooler_generate_config` | `false` | Master switch — render `pgbouncer.ini` into an operator-owned ConfigMap. |
| `connection_pooler_command` | _(unset)_ | Container `command` override; unset keeps the image entrypoint. Applied only when generating. |
| `connection_pooler_args` | `["/etc/pgbouncer/pgbouncer.ini"]` | Container `args`; applied only when generating. |
| `connection_pooler_auth_type` | `scram-sha-256` | `auth_type` written into the rendered config. |
| `connection_pooler_config_path` | `/etc/pgbouncer/pgbouncer.ini` | Mount path of the generated config. |

## 2. Enable the pooler on a Postgres cluster

The operator settings above only take effect for clusters that actually run a pooler. Enable it in the `postgresql` manifest:

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster
  namespace: default
spec:
  teamId: "acid"
  postgresql:
    version: "17"
  numberOfInstances: 2
  volume:
    size: 1Gi

  # Run a master connection pooler for this cluster.
  enableConnectionPooler: true
  # Optionally also pool replica connections:
  # enableReplicaConnectionPooler: true

  # Per-cluster pooler overrides are optional; defaults come from the operator config.
  connectionPooler:
    numberOfInstances: 2
    mode: "transaction"
```

## 3. Verify

```bash
# The operator-owned config map for the master pooler:
kubectl get configmap acid-minimal-cluster-pooler-config -o yaml

# Inspect the rendered pgbouncer.ini:
kubectl get configmap acid-minimal-cluster-pooler-config \
  -o jsonpath='{.data.pgbouncer\.ini}'

# Confirm the pooler pod mounts it and carries the checksum annotation:
kubectl get pod -l connection-pooler=acid-minimal-cluster-pooler \
  -o jsonpath='{.items[0].metadata.annotations.acid\.zalan\.do/pgbouncer-config-checksum}'
```

A rendered config looks roughly like:

```ini
[databases]
* = host=acid-minimal-cluster port=5432

[pgbouncer]
pool_mode = transaction
auth_type = scram-sha-256
auth_file = /etc/pgbouncer/userlist.txt
auth_query = SELECT * FROM pooler.user_lookup($1)
server_tls_sslmode = require
default_pool_size = 15
max_db_connections = 30
```

When the cluster has TLS configured (`spec.tls`), the operator additionally renders `client_tls_sslmode`, `client_tls_key_file`, and `client_tls_cert_file`.

## Notes

- `connection_pooler_command` and `connection_pooler_args` are applied **only** when `connection_pooler_generate_config` is `true`. With generation off, the image entrypoint runs unchanged.
- Changing any input that affects the rendered config (mode, auth type, sizes, TLS) updates the ConfigMap and changes the checksum annotation, which rolls the pooler pods automatically.
- The same parameters are available on the `OperatorConfiguration` CRD as `connection_pooler_generate_config`, `connection_pooler_command`, `connection_pooler_args`, `connection_pooler_auth_type`, and `connection_pooler_config_path`.
