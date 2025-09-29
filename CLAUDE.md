# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The provider-credential-controller is a Kubernetes controller that automatically updates cluster secrets when Provider Credential secrets are modified. It uses controller-runtime and operates with leader election to ensure only one instance runs at a time.

## Common Development Commands

### Building and Running
- `make compile` - Compile the Go code and generate binaries in `build/_output/`
- `go run ./cmd/manager/main.go` - Run the controller directly from source
- `make push` - Build and push Docker image (requires VERSION and REPO_URL env vars)

### Testing
- `make unit-tests` - Run unit tests for both controllers
- `make scale-up` - Create test environment with 3000 copied secrets
- `make scale-test` - Execute scale testing with multiple token updates
- `make scale-down` - Clean up scale test environment

### Deployment
- `oc apply -k deploy/controller` - Deploy to OpenShift cluster
- Controller deploys as single pod with leader election

## Architecture

### Core Components
- **Main Controller**: `cmd/manager/main.go` - Entry point with leader election setup
- **Provider Credential Controller**: `controllers/providercredential/` - Handles Provider Credential secret updates
- **Old Provider Connection Controller**: `controllers/oldproviderconnection/` - Legacy connection handling

### Key Concepts
- **Provider Credential Secrets**: Source secrets that trigger updates across copied secrets
- **Copied Secrets**: Target secrets that get updated when provider credentials change
- **Labels**: Uses `cluster.open-cluster-management.io/credentials` label to identify managed secrets
- **Hash Tracking**: Uses `credential-hash` annotation to track changes and avoid unnecessary updates

### Controller Architecture
- Built on controller-runtime framework
- Uses custom cache filtering to only watch labeled secrets
- Implements leader election for HA deployment
- Supports various provider types (AWS, Azure, GCP, RHV, etc.)

## Development Notes

### Go Module Structure
- Uses Go 1.23+ with toolchain 1.23.6
- Main dependencies: controller-runtime, client-go, zap logging
- Module path: `github.com/stolostron/provider-credential-controller`

### Pre-commit Requirements
- Run `make compile` before submitting PRs
- Controller includes scale testing for high-volume scenarios (3000+ secrets)

### Logging and Debugging
- Change log level in `cmd/manager/main.go:79` from `zapcore.InfoLevel` to `zapcore.DebugLevel`
- Uses structured logging with zap