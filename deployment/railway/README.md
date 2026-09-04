# Railway one-click template

Railway templates are composed in the Railway UI (there is no fully in-repo
multi-service spec), so publishing the Logchef template is a one-time manual
step. This runbook captures the exact configuration. It uses the shared images
in [`../oneclick/`](../oneclick/).

## Create the template

1. Go to <https://railway.com/workspace/templates> → **New Template**.
2. Add the three services below, then **Create Template**.
3. **Publish** the template so it appears in the marketplace and gets a public
   deploy URL.
4. Copy the deploy URL (format: `https://railway.com/deploy/<slug>`) and update
   the Railway button in the root `README.md` (it is currently commented out).

### Service 1: `logchef`

- **Source**: Docker Image → `ghcr.io/mr-karan/logchef:latest`
- **Networking**: generate a public domain; target port `8125`
- **Healthcheck path** (Settings → Deploy): `/api/v1/health`
- **Volume**: mount path `/data`
- **Variables**:

  | Variable | Value |
  | --- | --- |
  | `LOGCHEF_AUTH__LOCAL__ENABLED` | `true` |
  | `LOGCHEF_AUTH__LOCAL__ADMIN_EMAIL` | *(no default — prompted at deploy time)* |
  | `LOGCHEF_AUTH__LOCAL__ADMIN_PASSWORD` | `${{secret(16)}}` |
  | `LOGCHEF_AUTH__API_TOKEN_SECRET` | `${{secret(64, "abcdef0123456789")}}` |
  | `LOGCHEF_SQLITE__PATH` | `/data/logchef.db` |
  | `LOGCHEF_LOGGING__LEVEL` | `info` |
  | `LOGCHEF_OIDC__PROVIDER_URL` | *(empty — disables the baked-in dev Dex config)* |
  | `LOGCHEF_OIDC__AUTH_URL` | *(empty)* |
  | `LOGCHEF_OIDC__TOKEN_URL` | *(empty)* |

### Service 2: `clickhouse`

- **Source**: GitHub Repo → `https://github.com/mr-karan/logchef`, root directory
  `deployment/oneclick/clickhouse`
- **Networking**: private networking only (no public domain)
- **Volume**: mount path `/data/db`
- **Variables**:

  | Variable | Value |
  | --- | --- |
  | `CLICKHOUSE_USER` | `logchef` |
  | `CLICKHOUSE_PASSWORD` | `${{secret(32)}}` |
  | `CLICKHOUSE_DB` | `default` |

### Service 3: `vector` (demo log generator)

- **Source**: GitHub Repo → `https://github.com/mr-karan/logchef`, root directory
  `deployment/oneclick/vector`
- **Variables** (reference variables point at the clickhouse service):

  | Variable | Value |
  | --- | --- |
  | `CLICKHOUSE_HOST` | `${{clickhouse.RAILWAY_PRIVATE_DOMAIN}}` |
  | `CLICKHOUSE_PORT` | `8123` |
  | `CLICKHOUSE_USER` | `${{clickhouse.CLICKHOUSE_USER}}` |
  | `CLICKHOUSE_PASSWORD` | `${{clickhouse.CLICKHOUSE_PASSWORD}}` |

## After publishing

- Update the Railway button in the root `README.md` with the template URL.
- Users follow the first-login / connect-source steps in
  [`../oneclick/README.md`](../oneclick/README.md) (the private hostname on
  Railway is `<service>.railway.internal`, port `9000` for Logchef's native
  ClickHouse connection).

## Notes

- Railway injects `PORT` for the logchef HTTP target; the image listens on
  `8125` by default, which the networking target port above already matches.
- Consider a Railway volume for ClickHouse of at least 10 GB; the table has a
  30-day TTL so the demo data self-prunes.
