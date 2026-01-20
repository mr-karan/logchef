{
  description = "LogChef development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        go = pkgs.go_1_24;
        nodejs = pkgs.nodejs_22;
        pnpm = pkgs.nodePackages_latest.pnpm;
      in
      {
        devShells.default = pkgs.mkShell {
          name = "logchef-dev-shell";
          buildInputs = [
            go
            pkgs.gopls
            pkgs.golangci-lint
            pkgs.sqlc
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.clippy
            pkgs.rust-analyzer
            nodejs
            pnpm
            pkgs.just
            pkgs.git
            pkgs.docker
            pkgs.docker-compose
            pkgs.vector
            pkgs.sqlite
          ];

          shellHook = ''
            export GOBIN=$PWD/bin
            export GOPATH=$PWD/.gopath
            export GOCACHE=$PWD/.cache/go-build
            export GOMODCACHE=$PWD/.cache/go-mod
            export GOTMPDIR=$PWD/.cache/go-tmp
            export PNPM_HOME=$PWD/.pnpm
            export PATH="$GOBIN:$PNPM_HOME:$PATH"
            mkdir -p "$GOBIN" "$PNPM_HOME" "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR"
            export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
            echo "👉 Ready to build LogChef. Try: just build or just dev-docker"
          '';
        };
      });
}
