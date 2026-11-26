#!/usr/bin/env bash
# Sync the canonical Logchef skills into the CLI crate.
#
# The CLI embeds the skills at compile time with include_dir!. The embedded
# copies must live INSIDE the Cargo workspace (cli/) so `cross` release builds,
# which mount only the workspace, can find them. The canonical sources stay at
# .agents/skills (the published upstream the packaged skills sync from). Run
# this after editing a canonical skill; CI (rust-cli.yml) and the skills test
# in the CLI fail if the copies drift.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
crate="$repo_root/cli/crates/logchef-cli"
sync() {
	local src="$repo_root/.agents/skills/$1" dst="$crate/$2"
	rm -rf "$dst"
	mkdir -p "$dst"
	cp -r "$src/." "$dst/"
	echo "synced: $src -> $dst"
}
sync logchef skill
sync logchef-setup skill-setup
