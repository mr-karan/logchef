# Logchef Helm chart

The chart deploys Logchef, optional Dex, and an optional Altinity
ClickHouseInstallation. ClickHouse requires the Altinity operator CRDs.

## Authentication and secrets

Set `logchef.auth.existingSecret` to a persistent Secret containing the
`api-token-secret` key (or set `logchef.auth.apiTokenSecretKey`). The value must
contain at least 32 characters. Manage this Secret outside the chart. Changing
its value invalidates existing API tokens after Logchef loads the new value.

Offline rendering and GitOps must use an existing Secret. For native Helm
installs only, `logchef.auth.generateSecret: true` creates a random Secret on
first install and reuses it through Kubernetes lookup on upgrades. The chart
retains this Secret on uninstall. Offline rendering cannot look up the existing
Secret and generates a different value each time when generation is enabled.

Dex installs no demo login accounts. Configure `dex.connectors`, or explicitly
enable `dex.enablePasswordDB` and supply your own `dex.staticPasswords`. Set
`logchef.oidc.existingSecret` to a Secret with a `client-secret` key; Logchef and
Dex both load the same OIDC client credential from it. Configure your own
`logchef.config.auth.admin_emails`. No default client credential is installed.
For an external identity provider, disable Dex and configure the OIDC provider,
authorization, token, and redirect URLs under `logchef.config.oidc`.

For example, with an external identity provider:

```yaml
logchef:
  auth:
    existingSecret: logchef-api-token
  existingSecret: logchef-config
dex:
  enabled: false
clickhouse:
  enabled: false
```

The configuration Secret must contain a complete `config.toml`. It can also
contain the OIDC credentials. Environment variables in `logchef.extraEnv` can
reference external Secrets when individual credentials need separate storage.

## Configuration and storage

All maps under `logchef.config` become TOML sections. Nested maps, arrays of
tables, booleans, numbers, and strings are preserved. Empty OIDC URLs retain the
chart's computed defaults. Null is not a TOML value; omit optional keys instead.

`logchef.service.port` is the Service's public port. The container and named
probe port use `logchef.config.server.port`, which is the actual listener port.

SQLite Logchef deployments support one replica and default to `Recreate`, so
the old pod stops before a replacement mounts its volume. This causes downtime
during upgrades. Postgres deployments without a single-node PVC default to
`RollingUpdate`. Set `logchef.config.database.driver: postgres` and supply the
connection configuration. For multiple replicas, disable SQLite persistence or
use a ReadWriteMany volume. When using an existing configuration Secret, also
set the chart's database driver to match that file.
`logchef.strategy` can explicitly override the computed strategy.

Dex defaults to SQLite on a persistent volume and uses `Recreate`. Set
`dex.persistence.existingClaim` to reuse a PVC. For multiple replicas or larger
deployments, configure shared `dex.storage` (Postgres, MySQL, or Kubernetes).
External storage does not create or mount a SQLite PVC. `dex.extraEnv` supports
Secret references for storage and connector credentials.

Restricted namespaces must disable `logchef.dataPermsInit.enabled` and configure
pod/container security contexts. The storage driver or preconfigured volume
permissions must make `/data` writable. Dex supports `dex.podSecurityContext`
and `dex.securityContext` too. The bundled ClickHouse templates request extra
capabilities and are not a restricted-profile deployment.

## Upgrade notes

- Set `logchef.auth.existingSecret` to the existing generated Secret's name when
  moving to GitOps. Native Helm upgrades can still reuse a generated Secret
  through lookup without enabling new generation. Preserve its value.
- Demo Dex accounts are removed. Supply connectors or your own static users
  and an OIDC client credential before upgrading. Dex's previous `emptyDir`
  database cannot be preserved after its pod is deleted; the first replacement
  uses a fresh persistent DB.
- ClickHouse names now include the release name by default. **For an existing
  installation, set `clickhouse.name: logchef` before upgrading** to retain its
  ClickHouseInstallation and data. Preserve any existing custom name instead.
- Short Logchef and Dex resource names remain unchanged. Long names reserve
  their suffixes and include a hash. Deployments that relied on truncated long
  names need a planned resource migration.
- The Logchef image is pinned to `v2.3.1`, matching this chart's `appVersion`.
  Set `logchef.image.tag` to retain a newer explicitly chosen image on upgrade.
- Extra pod security fields now take effect. SQLite deployments use a stop/start
  upgrade rather than overlapping pods.

## Checks

Helm 3.19.0 is pinned in CI. It supports the chart's template functions without
requiring Helm 4. The regression suite runs real Helm renders and parses the
generated TOML and YAML.

```sh
helm lint deployment/helm --strict \
  --set logchef.auth.existingSecret=test-api-token --set dex.enabled=false
go test ./deployment/helm -v
```

Set `HELM_BIN` if Helm is not on PATH. The Go tests skip when Helm is unavailable;
CI installs Helm and always runs them.
