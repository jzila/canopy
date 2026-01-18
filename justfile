# Canopy build recipes
# Run `just --list` to see available commands

# Default recipe: full rebuild
default: build

# Full build: dashboard + go binary
build: dashboard go-build
    @echo "Build complete"

# Build dashboard (TypeScript → dist/)
dashboard:
    @echo "Building dashboard..."
    cd web/dashboard && npm run build

# Build Go binary (embeds dist/)
go-build:
    @echo "Building Go binary..."
    go build ./...

# Quick go build (skip dashboard, use existing dist/)
go:
    go build ./...

# Run tests
test: test-go test-dashboard

test-go:
    go test ./...

test-dashboard:
    cd web/dashboard && npm test -- --run

# Start daemon (rebuilds everything first)
run: build
    canopy daemon restart

# Restart daemon without rebuilding
restart:
    canopy daemon restart

# Development: run vite dev server (requires daemon running separately)
dev:
    cd web/dashboard && npm run dev

# Clean build artifacts
clean:
    rm -rf web/dashboard/dist
    rm -rf web/dashboard/node_modules/.vite
    go clean ./...

# Clean stale JS files from src/ (fixes tsc accidents)
clean-stale:
    find web/dashboard/src -name "*.js" -type f -delete
    find web/dashboard/src -name "*.js.map" -type f -delete
    find web/dashboard/src -name "*.d.ts" -type f ! -name "vite-env.d.ts" -delete
    @echo "Cleaned stale transpiled files"

# Install dashboard dependencies
install:
    cd web/dashboard && npm install

# Check for issues (stale files, build errors)
check:
    #!/usr/bin/env bash
    set -e
    echo "Checking for stale JS files in src/..."
    STALE=$(find web/dashboard/src -name "*.js" -type f 2>/dev/null || true)
    if [ -n "$STALE" ]; then
        echo "ERROR: Found stale .js files:"
        echo "$STALE"
        echo "Run: just clean-stale"
        exit 1
    fi
    echo "Checking Go build..."
    go build ./...
    echo "Checking dashboard types..."
    cd web/dashboard && npm run type-check || true
    echo "All checks passed"

# Full rebuild from scratch
rebuild: clean install build
