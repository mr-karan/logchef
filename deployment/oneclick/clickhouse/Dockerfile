# ClickHouse image for one-click deploys (Render blueprint, Railway template).
#
# Builds on the official image and adds:
#   - the `logs` table schema (auto-created on first boot via initdb)
#   - a listen-host override so private-network peers can connect
#   - a persistent-data layout compatible with PaaS volume mounts at /data/db
FROM clickhouse/clickhouse-server:24.8.4

COPY config.d/ /etc/clickhouse-server/config.d/
COPY initdb/ /docker-entrypoint-initdb.d/

# PaaS volumes (Render disks, Railway volumes) mount at /data/db. Point
# ClickHouse's data dir there so logs survive restarts and redeploys.
RUN mkdir -p /data/db \
    && chown -R clickhouse:clickhouse /data/db \
    && rm -rf /var/lib/clickhouse \
    && ln -s /data/db /var/lib/clickhouse
