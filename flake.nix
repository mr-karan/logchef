{
  description = "Logchef development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      rust-overlay,
    }:
    let
      # nixos-unstable no longer supports x86_64-darwin.
      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor =
        system:
        import nixpkgs {
          inherit system;
          overlays = [ rust-overlay.overlays.default ];
        };

      toolsFor =
        pkgs: with pkgs; [
          go_1_27
          sqlc
          golangci-lint
          bun
          just
          (rust-bin.fromRustupToolchainFile ./rust-toolchain.toml)
          nixfmt
        ];
    in
    {
      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            name = "logchef-dev";
            packages = toolsFor pkgs;
            # Use the shell's Go instead of downloading the go.mod toolchain.
            GOTOOLCHAIN = "local";
          };
        }
      );

      # The shell must carry the versions CI pins, or `just lint` refuses to run.
      checks = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          ci-tool-versions =
            pkgs.runCommand "logchef-ci-tool-versions"
              {
                nativeBuildInputs = [
                  pkgs.golangci-lint
                  pkgs.sqlc
                ];
              }
              ''
                export HOME="$TMPDIR"
                workflow=${./.github/workflows/go-tests.yml}

                want_lint=$(grep -oE 'version: v[0-9]+\.[0-9]+\.[0-9]+' "$workflow" | head -1 | cut -d' ' -f2)
                have_lint="v$(golangci-lint version --short)"
                want_sqlc=$(grep -oE 'sqlc/cmd/sqlc@v[0-9]+\.[0-9]+\.[0-9]+' "$workflow" | head -1 | cut -d@ -f2)
                have_sqlc=$(sqlc version)

                status=0
                if [ "$have_lint" != "$want_lint" ]; then
                  echo "golangci-lint: shell has $have_lint, CI pins $want_lint" >&2
                  status=1
                fi
                if [ "$have_sqlc" != "$want_sqlc" ]; then
                  echo "sqlc: shell has $have_sqlc, CI pins $want_sqlc" >&2
                  status=1
                fi
                [ "$status" -eq 0 ] && touch "$out"
                exit "$status"
              '';
        }
      );

      formatter = forAllSystems (system: (pkgsFor system).nixfmt);
    };
}
