# Vector image for one-click deploys (Render blueprint, Railway template).
#
# Ships synthetic syslog into ClickHouse so a freshly deployed stack has data
# to explore immediately. The image carries its config; connection details are
# injected at runtime via CLICKHOUSE_* environment variables (Vector expands
# ${VAR} placeholders when the config is loaded).
#
# Pinned below 0.57: newer Vector versions disable ${VAR} interpolation in
# config files by default and would need
# VECTOR_DANGEROUSLY_ALLOW_ENV_VAR_INTERPOLATION=true. Bump the tag alongside
# that env var if you ever upgrade.
FROM timberio/vector:0.45.0-debian

COPY vector.toml /etc/vector/vector.toml

# The base image's default CMD loads /etc/vector/vector.yaml — point it at ours.
CMD ["--config", "/etc/vector/vector.toml"]
