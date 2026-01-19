{
  description = "Canopy - parallel agent orchestration for Claude Code";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachSystem [
      "x86_64-linux"
      "aarch64-linux"
      "x86_64-darwin"
      "aarch64-darwin"
    ] (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Build the dashboard with npm
        dashboard = pkgs.buildNpmPackage {
          pname = "canopy-dashboard";
          version = "0.1.0";
          src = ./web/dashboard;

          npmDepsHash = "sha256-n/tmmdgUHQIC1b35tqIJ6gGWnkeelGEnNxbyi1v4wwA=";

          # Build produces dist/ directory
          buildPhase = ''
            runHook preBuild
            npm run build
            runHook postBuild
          '';

          installPhase = ''
            runHook preInstall
            cp -r dist $out
            runHook postInstall
          '';
        };

        # Build the Go binary with embedded dashboard
        canopy = pkgs.buildGoModule {
          pname = "canopy";
          version = "0.1.0";
          src = ./.;

          vendorHash = "sha256-KC/L4mLc/k7iZabUqzkyTK4SNxyYG8LbXa47oUWFBzY=";

          # Inject pre-built dashboard into web/dashboard/dist before Go build
          preBuild = ''
            mkdir -p web/dashboard
            cp -r ${dashboard} web/dashboard/dist
          '';

          # Build the main binary
          subPackages = [ "cmd/canopy" ];

          # modernc.org/sqlite is pure Go, no CGO needed
          env.CGO_ENABLED = "0";

          ldflags = [
            "-s"
            "-w"
          ];

          meta = with pkgs.lib; {
            description = "Parallel agent orchestration for Claude Code";
            homepage = "https://github.com/jzila/canopy";
            license = licenses.mit;
            maintainers = [ ];
            mainProgram = "canopy";
          };
        };
      in
      {
        packages = {
          default = canopy;
          inherit canopy dashboard;
        };

        # Expose the app for nix run
        apps.default = flake-utils.lib.mkApp {
          drv = canopy;
        };
      }
    );
}
