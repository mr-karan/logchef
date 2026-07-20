#!/usr/bin/env bash
# Sync the canonical Logchef skill into the CLI crate.
#
# The CLI embeds the skill at compile time with include_dir!. The embedded copy
# must live INSIDE the Cargo workspace (cli/) so `cross` release builds — which
# mount only the workspace — can find it. The canonical source stays at
# .agents/skills/logchef (the published upstream the packaged skill syncs from).
# Run this after editing the canonical skill; CI (rust-cli.yml) fails if the two
# copies drift.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
src="$repo_root/.agents/skills/logchef"
dst="$repo_root/cli/crates/logchef-cli/skill"
rm -rf "$dst"
mkdir -p "$dst"
cp -r "$src/." "$dst/"
echo "synced: $src -> $dst"
