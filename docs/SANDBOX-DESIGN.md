# Canopy Sandbox Design

This document describes the sandboxing architecture for Canopy's parallel agent orchestrator, including security analysis, design decisions, and the interactive bootstrapping system.

## Table of Contents
1. [Overview](#overview)
2. [Current Architecture](#current-architecture)
3. [Security Analysis](#security-analysis)
4. [Proposed Architecture](#proposed-architecture)
5. [Interactive Bootstrapping](#interactive-bootstrapping)
6. [Implementation Phases](#implementation-phases)

---

## Overview

Canopy orchestrates multiple Claude Code agents running in parallel, each operating in an isolated OverlayFS workspace. The goal is to run agents with `--dangerously-skip-permissions` (enabling truly unblocked agentic coding) while restricting what agents can do **outside** their overlay workspace.

### Design Goals

| Goal | Description |
|------|-------------|
| **Isolation** | Each agent sees only its workspace, not others' changes |
| **Security** | Agents cannot access sensitive files outside workspace |
| **Functionality** | Agents must have access to tools (compilers, git, etc.) |
| **Configurability** | Per-project configuration of what to expose |
| **Usability** | One-time interactive setup, easily modifiable |

---

## Current Architecture

### Workspace Isolation: OverlayFS

Each agent operates in a copy-on-write filesystem:

```
┌────────────────────────────────────────────────────┐
│                  MergedDir (Agent View)            │
│  /tmp/canopy-overlay-{id}/merged                   │
├────────────────────────────────────────────────────┤
│           UpperDir (Agent's Changes)               │
│  /tmp/canopy-overlay-{id}/upper                    │
├────────────────────────────────────────────────────┤
│           LowerDir (Original Repo - Read Only)     │
│  /home/user/project                                │
└────────────────────────────────────────────────────┘
```

**Implementation:** `pkg/sandbox/overlay.go`, `pkg/sandbox/overlay_linux.go`

- Uses kernel overlayfs or falls back to fuse-overlayfs
- Creates whiteouts to hide `.claude/` directory (parent session config)
- Copies credentials (`.claude.json`, `.gitconfig`) to upper layer
- Tracks file changes via upper directory diff

### Optional Bwrap Sandboxing

When `--sandbox` flag is passed:

**Implementation:** `pkg/sandbox/bwrap.go`

```go
args := []string{
    "--unshare-user",     // User namespace
    "--unshare-pid",      // PID namespace
    "--unshare-ipc",      // IPC namespace
    "--unshare-uts",      // UTS namespace
    "--die-with-parent",  // Kill on parent exit
    "--cap-drop", "ALL",  // Drop all capabilities
    "--tmpfs", "/tmp",    // Fresh /tmp
    "--bind", mergedDir, "/workspace",
    "--chdir", "/workspace",
    "--dev", "/dev",      // Minimal /dev
    "--proc", "/proc",    // Minimal /proc
}

// Read-only system mounts
for _, bind := range []string{"/nix", "/usr", "/lib", "/lib64", "/bin",
                              "/etc/resolv.conf", "/etc/ssl/certs"} {
    args = append(args, "--ro-bind", bind, bind)
}
```

### Environment Filtering

**Implementation:** `pkg/agent/executor.go:271-293`

Only these environment variables pass through:
- `ANTHROPIC_*` - API credentials
- `PATH` - Executable search
- `LANG`, `LC_*` - Locale
- `TERM` - Terminal type
- `TMPDIR`, `TZ` - Temp/timezone

### Current Defaults

| Feature | Default | With `--sandbox` |
|---------|---------|------------------|
| OverlayFS isolation | Yes | Yes |
| Environment filtering | Yes | Yes |
| `.claude/` hidden | Yes | Yes |
| Namespace isolation | **No** | Yes |
| Capability drop | **No** | Yes |
| Resource limits | Soft (Linux rlimits) | 4GB mem, 100 procs |
| Network access | **Unrestricted** | **Unrestricted** |
| Filesystem outside overlay | **Unrestricted** | Read-only system paths |

---

## Security Analysis

### Threat Model

When running with `--dangerously-skip-permissions`, Claude Code can:
- Execute arbitrary bash commands
- Read/write any accessible file
- Make network connections
- Spawn processes

**We trust the agent's intent** (it's working on your code) but want to **contain the blast radius** of mistakes or prompt injection.

### Risk Matrix (Without Full Sandbox)

| Risk | Severity | Current Mitigation | Gap |
|------|----------|-------------------|-----|
| Read `~/.ssh/id_rsa` | Critical | None | Agent can read any user file |
| Read `~/.aws/credentials` | Critical | None | Cloud credential theft |
| Exfiltrate data via network | Critical | None | Unrestricted outbound |
| Write to `~/.bashrc` | High | None | Persistent compromise |
| Read other project dirs | Medium | None | IP leakage |
| Fork bomb | Medium | rlimits (100 procs) | Soft limit only |
| Fill disk | Medium | None | No disk quota |

### Risk Matrix (With `--sandbox`)

| Risk | Severity | Current Mitigation | Gap |
|------|----------|-------------------|-----|
| Read `~/.ssh/id_rsa` | Critical | Not bind-mounted | **Resolved** |
| Read `~/.aws/credentials` | Critical | Not bind-mounted | **Resolved** |
| Exfiltrate via network | Critical | None | **Still unrestricted** |
| Read `/etc/passwd` | Low | Read-only bind | Acceptable |
| Read system binaries | Low | Read-only bind | Required for operation |
| Fork bomb | Low | 100 proc limit | Acceptable |

### Key Security Gap: Network

Even with bwrap, agents have unrestricted network access. This enables:
- Data exfiltration to attacker servers
- Reverse shells
- SSRF against internal services
- Cryptocurrency mining

**This is the primary gap to address.**

---

## Proposed Architecture

### Layered Security Model

```
┌─────────────────────────────────────────────────────────────┐
│                    Canopy Orchestrator                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Agent Spawn Request                                        │
│         │                                                   │
│         ▼                                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              Project Config (.canopy/sandbox.toml)  │    │
│  │  - Extra tools needed                               │    │
│  │  - Config directories to expose                     │    │
│  │  - Network policy                                   │    │
│  └────────────────────────┬────────────────────────────┘    │
│                           │                                 │
│                           ▼                                 │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                 Bubblewrap Sandbox                  │    │
│  │  - User/PID/IPC/UTS/NET namespaces                  │    │
│  │  - Capability drop                                  │    │
│  │  - Seccomp filter (optional)                        │    │
│  └────────────────────────┬────────────────────────────┘    │
│                           │                                 │
│                           ▼                                 │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                   OverlayFS Mount                   │    │
│  │  - Workspace at /workspace                          │    │
│  │  - Configured paths from sandbox.toml               │    │
│  └────────────────────────┬────────────────────────────┘    │
│                           │                                 │
│                           ▼                                 │
│  ┌─────────────────────────────────────────────────────┐    │
│  │        Claude Code (--dangerously-skip-permissions) │    │
│  └─────────────────────────────────────────────────────┘    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Namespace Configuration

#### Default (Secure)
```go
bwrapArgs := []string{
    "--unshare-all",       // All namespaces including NET
    "--share-net",         // Then selectively share net (or not)
    "--die-with-parent",
    "--cap-drop", "ALL",
}
```

#### Network Namespace Options

| Option | Use Case | Implementation |
|--------|----------|----------------|
| `--unshare-net` | Full isolation (no network) | Default for untrusted |
| `--share-net` | Allow network (current) | Required for API calls |
| Proxy (future) | Controlled network | slirp4netns + allowlist |

**Decision:** Default to `--share-net` since Claude Code needs to call Anthropic API. Network proxy is future work.

### Path Exposure Categories

The sandbox needs to expose certain paths for agents to function:

#### 1. System Paths (Read-Only)
Required for basic operation:
```
/nix                    # Nix-installed tools
/usr                    # System binaries
/lib, /lib64            # Shared libraries
/bin                    # Basic binaries
/etc/resolv.conf        # DNS
/etc/ssl/certs          # TLS certificates
/etc/ca-certificates    # CA certs
```

#### 2. Tool-Specific Paths (Read-Only)
Project-dependent, discovered during bootstrap:
```
~/.cargo                # Rust toolchain
~/.rustup               # Rust versions
~/.local/share/mise     # Mise tool versions
~/.nvm                  # Node versions
~/.pyenv                # Python versions
~/.goenv                # Go versions
/usr/local/go           # System Go
```

#### 3. Config Paths (Read-Only)
Credentials and config needed by tools:
```
~/.claude.json          # Claude credentials (auto-copied to overlay)
~/.claude/              # Claude config dir (selectively copied)
~/.gitconfig            # Git identity (auto-copied to overlay)
~/.npmrc                # npm registry config
~/.docker/config.json   # Docker registry auth
~/.netrc                # Generic credentials
~/.config/gh/           # GitHub CLI auth
```

#### 4. Cache Paths (Read-Write via Bind)
Shared caches to avoid re-downloading:
```
~/.npm                  # npm cache
~/.cache/pip            # pip cache
~/.cache/go-build       # Go build cache
~/.cargo/registry       # Cargo crate cache
~/.local/share/pnpm     # pnpm cache
```

---

## Interactive Bootstrapping

### Overview

First-time setup for a project that:
1. Auto-detects project type and required tools
2. Discovers config/cache paths needed
3. Generates a config file
4. Explains each choice to the user
5. Allows customization

### Config File: `.canopy/sandbox.toml`

```toml
# Canopy Sandbox Configuration
# Generated by: canopy init
# Documentation: https://github.com/jzila/canopy/docs/SANDBOX-DESIGN.md

[sandbox]
# Enable bubblewrap sandboxing (recommended)
enabled = true

# Network access policy
# Options: "allow" (default), "deny", "proxy" (future)
network = "allow"

[resources]
# Resource limits for each agent
max_memory = "4GB"
max_processes = 100
max_open_files = 1024
max_disk = "10GB"  # Future: disk quota

[paths]
# Additional read-only paths to expose to agents
# These are auto-detected based on your project

# Tool paths (detected: node, rust)
read_only = [
    "~/.nvm",
    "~/.cargo",
    "~/.rustup",
]

# Config paths (detected: npm, git)
# These are COPIED to the overlay (agent gets a copy, not the original)
copy_configs = [
    "~/.npmrc",
    "~/.gitconfig",    # Always included
    "~/.claude.json",  # Always included
]

# Cache paths (bind-mounted read-write for performance)
# Warning: Agents can modify these shared caches
cache_mounts = [
    "~/.npm",
    "~/.cargo/registry",
]

[paths.extra]
# Add custom paths here
# read_only = ["/opt/custom-tool"]
# copy_configs = ["~/.custom-tool-rc"]

[security]
# Paths that are NEVER exposed (even if requested)
# These are in addition to built-in blocklist
blocked = [
    "~/.ssh",
    "~/.gnupg",
    "~/.aws",
    "~/.azure",
    "~/.config/gcloud",
]
```

### Bootstrap Flow

```
$ canopy init

Canopy Sandbox Bootstrapper
===========================

Scanning project for tool requirements...

Detected Project Type: Node.js + Rust (monorepo)

Tools Found:
  [x] node v20.10.0 (~/.nvm/versions/node/v20.10.0)
  [x] npm 10.2.3 (~/.nvm/versions/node/v20.10.0/bin/npm)
  [x] cargo 1.75.0 (~/.cargo/bin/cargo)
  [x] rustc 1.75.0 (~/.rustup/toolchains/stable-x86_64-unknown-linux-gnu)
  [x] git 2.43.0 (/usr/bin/git)

Config Files Detected:
  [x] ~/.npmrc (npm registry config)
  [x] ~/.gitconfig (git identity)
  [x] ~/.cargo/config.toml (cargo settings)

Cache Directories:
  [x] ~/.npm (npm package cache, 2.3GB)
  [x] ~/.cargo/registry (crate cache, 1.8GB)

Recommended Sandbox Configuration:
──────────────────────────────────

Read-Only Tool Paths:
  These paths will be visible to agents but not writable.

  ~/.nvm              Node.js version manager
  ~/.cargo            Cargo binaries and config
  ~/.rustup           Rust toolchains

Config Copies:
  These files will be COPIED to each agent's workspace.
  Agents can modify the copy but not your original.

  ~/.npmrc            npm registry authentication
  ~/.gitconfig        Git user identity
  ~/.cargo/config.toml  Cargo build settings

Shared Cache Mounts:
  These are mounted read-write for performance.
  ⚠️  Agents CAN modify these shared caches.

  ~/.npm              npm package cache
  ~/.cargo/registry   Cargo crate downloads

Security Blocklist (never exposed):
  ~/.ssh              SSH keys - BLOCKED
  ~/.aws              AWS credentials - BLOCKED
  ~/.gnupg            GPG keys - BLOCKED

Proceed with this configuration? [Y/n/customize]
> y

Writing .canopy/sandbox.toml...
Done! Run 'canopy run' to start parallel agents.

Tip: Edit .canopy/sandbox.toml to customize paths.
     Run 'canopy init --reconfigure' to re-run this wizard.
```

### Detection Heuristics

#### Project Type Detection

```go
type ProjectDetector struct{}

func (d *ProjectDetector) Detect(workdir string) []ProjectType {
    var types []ProjectType

    // Node.js
    if exists("package.json") {
        types = append(types, NodeJS)
    }

    // Rust
    if exists("Cargo.toml") {
        types = append(types, Rust)
    }

    // Go
    if exists("go.mod") {
        types = append(types, Go)
    }

    // Python
    if exists("pyproject.toml") || exists("requirements.txt") ||
       exists("setup.py") || exists("Pipfile") {
        types = append(types, Python)
    }

    // etc...
    return types
}
```

#### Tool Path Discovery

```go
type ToolDiscovery struct{}

func (d *ToolDiscovery) DiscoverPaths(projectTypes []ProjectType) PathConfig {
    config := PathConfig{}

    for _, pt := range projectTypes {
        switch pt {
        case NodeJS:
            // Check for various Node version managers
            if dir := expandAndCheck("~/.nvm"); dir != "" {
                config.ReadOnly = append(config.ReadOnly, dir)
            }
            if dir := expandAndCheck("~/.volta"); dir != "" {
                config.ReadOnly = append(config.ReadOnly, dir)
            }
            if dir := expandAndCheck("~/.npm"); dir != "" {
                config.CacheMounts = append(config.CacheMounts, dir)
            }
            if file := expandAndCheck("~/.npmrc"); file != "" {
                config.CopyConfigs = append(config.CopyConfigs, file)
            }

        case Rust:
            if dir := expandAndCheck("~/.cargo"); dir != "" {
                config.ReadOnly = append(config.ReadOnly, dir)
            }
            if dir := expandAndCheck("~/.rustup"); dir != "" {
                config.ReadOnly = append(config.ReadOnly, dir)
            }
            if dir := expandAndCheck("~/.cargo/registry"); dir != "" {
                config.CacheMounts = append(config.CacheMounts, dir)
            }

        case Go:
            // Check GOPATH, GOROOT, GOCACHE
            if gopath := os.Getenv("GOPATH"); gopath != "" {
                config.ReadOnly = append(config.ReadOnly, gopath)
            }
            if cache := expandAndCheck("~/.cache/go-build"); cache != "" {
                config.CacheMounts = append(config.CacheMounts, cache)
            }

        // etc...
        }
    }

    return config
}
```

### Config Modification

Users can edit `.canopy/sandbox.toml` directly:

```toml
# User added custom database tools
[paths]
read_only = [
    "~/.nvm",
    "~/.cargo",
    "/opt/postgres-16/bin",  # Added: local postgres tools
]

copy_configs = [
    "~/.npmrc",
    "~/.gitconfig",
    "~/.pgpass",  # Added: postgres credentials
]
```

Or via CLI:

```bash
# Add a path
canopy config add-path --read-only /opt/custom-tool
canopy config add-path --copy ~/.custom-rc
canopy config add-path --cache ~/.custom-cache

# Remove a path
canopy config remove-path ~/.npm

# Show current config
canopy config show

# Validate config
canopy config validate
```

---

## Implementation Phases

### Phase 0: Current State (Functional Focus)
**Goal:** Get the orchestrator working reliably before locking down security.

- [x] OverlayFS isolation
- [x] Basic environment filtering
- [x] Optional bwrap (opt-in via `--sandbox`)
- [ ] Stabilize merge logic
- [ ] Handle git conflicts

### Phase 1: Bootstrap System
**Goal:** Enable per-project sandbox configuration.

1. Implement project type detection
2. Implement tool/config discovery
3. Create interactive `canopy init` wizard
4. Parse `.canopy/sandbox.toml` in executor
5. Apply config to bwrap args

### Phase 2: Default to Sandbox
**Goal:** Make sandboxing the default, require opt-out.

1. Flip default: `--sandbox=true`, add `--no-sandbox`
2. Update bwrap to use config paths
3. Implement config file copying (vs bind mount)
4. Implement cache mount handling

### Phase 3: Hardening (Future)
**Goal:** Address remaining security gaps.

1. Seccomp filter for dangerous syscalls
2. Network namespace with proxy (slirp4netns)
3. Disk quotas
4. Audit logging of operations outside workspace

---

## Appendix: Bwrap Reference

### Full Proposed Bwrap Invocation

```bash
bwrap \
    # Namespaces
    --unshare-user \
    --unshare-pid \
    --unshare-ipc \
    --unshare-uts \
    --unshare-cgroup \
    # Note: NOT --unshare-net (need API access)

    # Lifecycle
    --die-with-parent \

    # Capabilities
    --cap-drop ALL \

    # Resource limits
    --rlimit as=4294967296 \    # 4GB virtual memory
    --rlimit nproc=100 \        # 100 processes
    --rlimit nofile=1024 \      # 1024 file descriptors

    # Workspace (from overlay)
    --bind /tmp/canopy-xxx/merged /workspace \
    --chdir /workspace \

    # System paths (read-only)
    --ro-bind /nix /nix \
    --ro-bind /usr /usr \
    --ro-bind /lib /lib \
    --ro-bind /lib64 /lib64 \
    --ro-bind /bin /bin \
    --ro-bind /etc/resolv.conf /etc/resolv.conf \
    --ro-bind /etc/ssl /etc/ssl \

    # Tool paths from config (read-only)
    --ro-bind ~/.nvm ~/.nvm \
    --ro-bind ~/.cargo ~/.cargo \
    --ro-bind ~/.rustup ~/.rustup \

    # Cache mounts from config (read-write)
    --bind ~/.npm ~/.npm \
    --bind ~/.cargo/registry ~/.cargo/registry \

    # Minimal virtual filesystems
    --proc /proc \
    --dev /dev \
    --tmpfs /tmp \

    # Environment
    --setenv HOME /workspace \
    --setenv PATH "..." \
    --setenv ANTHROPIC_API_KEY "..." \

    # Command
    -- claude --print --output-format json --dangerously-skip-permissions "..."
```

### Security Blocklist (Hardcoded)

These paths are NEVER exposed regardless of config:

```go
var SecurityBlocklist = []string{
    "~/.ssh",
    "~/.gnupg",
    "~/.aws",
    "~/.azure",
    "~/.config/gcloud",
    "~/.kube",
    "~/.docker/config.json",  // Contains registry auth
    "~/.netrc",
    "~/.password-store",
    "~/.local/share/keyrings",
    "/etc/shadow",
    "/etc/sudoers",
}
```

---

## References

- [Bubblewrap Documentation](https://github.com/containers/bubblewrap)
- [OverlayFS Kernel Docs](https://www.kernel.org/doc/html/latest/filesystems/overlayfs.html)
- [Linux Namespaces](https://man7.org/linux/man-pages/man7/namespaces.7.html)
- [Seccomp BPF](https://www.kernel.org/doc/html/latest/userspace-api/seccomp_filter.html)
