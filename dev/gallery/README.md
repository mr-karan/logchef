# Dashboard gallery

These scripts build the dashboards shown on the docs page
`docs/src/content/docs/features/dashboard-gallery.mdx` and take its screenshots.
Every run regenerates the data relative to now, so the screenshots always show
the last 24 hours and the last 7 days.

| File | What it does |
| :--- | :--- |
| `data.sql` | Creates the `gallery` database in ClickHouse with `http_requests` (7 days of API traffic) and `orders` (7 days of orders in INR) |
| `victorialogs.sql` | A ClickHouse query that prints 7 days of worker logs as JSON lines for VictoriaLogs |
| `provision.ts` | Loads both data sets, creates the Storefront team and its three sources, and creates or replaces the three dashboards |
| `shots.sh` | Signs in through Dex and writes PNG, WebP and AVIF screenshots to `docs/public/screenshots/dashboard-*` |

## Run it

1. Start the dev stack and sign in once as `admin@logchef.internal` so the
   user exists:

   ```bash
   just dev-setup
   just run-backend     # terminal 1
   just dev-seed        # API token for dev@localhost
   just run-frontend    # terminal 2
   ```

2. Build the data, the sources and the dashboards:

   ```bash
   nix develop --command bun dev/gallery/provision.ts
   ```

   It prints the dashboard IDs as JSON. A rerun takes about 5 seconds and
   replaces the data and the dashboards in place.

3. Take the screenshots. `cwebp` and `avifenc` come from Nix:

   ```bash
   nix shell --inputs-from . nixpkgs#libwebp nixpkgs#libavif --command dev/gallery/shots.sh
   ```

   To retake some shots, list them in `SHOTS`, for example
   `SHOTS=business,builder-stat`. The names are `api-health`,
   `api-health-week`, `business`, `worker-logs`, `builder-chart`,
   `builder-stat`, `tabs-edit` and `stat-thresholds`. `KEEP_OPEN=1` leaves the
   browser session `logchef-gallery` open after the run.

Open each PNG before you commit it. A panel that shows an error or "No data"
in one shot means the run went wrong, not the dashboard.

## Settings

| Variable | Default | Used by |
| :--- | :--- | :--- |
| `LOGCHEF_URL` | `http://localhost:8125` | both |
| `LOGCHEF_TOKEN` | the `just dev-seed` token | both |
| `CLICKHOUSE_URL` | `http://localhost:8123` | `provision.ts` |
| `VICTORIALOGS_URL` | `http://localhost:9428` | `provision.ts` |
| `GALLERY_VIEWER` | `admin@logchef.internal` | `provision.ts`, added to the team |
| `BASE_URL` | `http://localhost:5173` | `shots.sh` |
| `EMAIL`, `PASSWORD` | the Dex dev login | `shots.sh` |

Screenshots use a 1440 pixel wide viewport at device scale 2, the
`Asia/Kolkata` timezone and the "local" timezone preference. Full dashboard
shots set the viewport height to the dashboard's height.

## How reruns stay clean

- ClickHouse tables are dropped and created again.
- Teams, sources and dashboards are matched by name. A dashboard is updated
  with its current revision, and extra copies with the same name are deleted.
- The dev VictoriaLogs runs without `-delete.enable`, so old logs cannot be
  removed. Each run writes to a new tenant (`ProjectID` is the Unix time of the
  run) and points the Worker logs source at it. Older tenants expire with the
  7-day retention.
- The dashboards turn the result cache off (TTL 0), so every shot runs its
  panels live. A dashboard with more panels than the per-user preview limit
  still loads, because panel queries are admitted as dashboard queries.
