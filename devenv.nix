{ pkgs, lib, config, inputs, ... }:

{
  # https://devenv.sh/basics/
  env.GREET = "devenv";

  # https://devenv.sh/packages/
  packages = [
    pkgs.sqlite
    pkgs.git
    pkgs.claude-code
    pkgs.fuse-overlayfs  # Rootless OverlayFS fallback
    inputs.beads.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  # https://devenv.sh/languages/
  languages.go.enable = true;

  # https://devenv.sh/processes/
  # processes.dev.exec = "${lib.getExe pkgs.watchexec} -n -- ls -la";

  # https://devenv.sh/services/
  # services.postgres.enable = true;

  # https://devenv.sh/scripts/
  scripts.hello.exec = ''
    echo hello from $GREET
  '';

  scripts.build.exec = ''
    go build -o "$DEVENV_ROOT/canopy" ./cmd/canopy
    echo "Built: canopy"
  '';

  # https://devenv.sh/basics/
  enterShell = ''
    export PATH="$DEVENV_ROOT:$PATH"
    hello         # Run scripts directly
    git --version # Use packages
  '';

  # https://devenv.sh/tasks/
  # tasks = {
  #   "myproj:setup".exec = "mytool build";
  #   "devenv:enterShell".after = [ "myproj:setup" ];
  # };

  # https://devenv.sh/tests/
  enterTest = ''
    echo "Running tests"
    git --version | grep --color=auto "${pkgs.git.version}"
  '';

  # https://devenv.sh/git-hooks/
  git-hooks.hooks.go-build = {
    enable = true;
    name = "go build";
    description = "Verify Go code compiles";
    entry = "go build ./...";
    pass_filenames = false;
    files = "\\.go$";
  };

  # See full reference at https://devenv.sh/reference/options/
}
