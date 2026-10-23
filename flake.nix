{
  description = "D4 Department Exhibition";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
    in {
      devShells = nixpkgs.lib.genAttrs systems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell {
            packages = with pkgs; [
              arduino-cli

              go_latest
              gopls
              gotools
              golangci-lint
            ];
            
            shellHook = ''
              export GOPATH="$(git rev-parse --show-toplevel 2>/dev/null || pwd)/.go"
              export PATH="$GOPATH/bin:$PATH"
              echo "GOPATH: $GOPATH"
              arduino-cli version
              go version
            '';
          };
        }
      );
    };
}
