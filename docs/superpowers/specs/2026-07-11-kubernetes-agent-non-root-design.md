# Kubernetes Agent Non-Root Design

## Problem

The managed agent runner uses Claude Code's `bypassPermissions` mode. Claude
Code rejects that mode when the process has root privileges. The server's local
Docker paths work because both persistent execs and ephemeral containers
explicitly run as the `node` user. Kubernetes agent pods currently leave the
main container user unspecified, so they inherit the agent image's final
`USER root` directive and fail before the first agent turn. Artifact upload then
runs as normal cleanup, which explains the misleading successful upload log.

## Design

Align Kubernetes execution with the proven local Docker contract. Keep the
workspace init container running as root so it can create and `chown` the
project directory. Set the main agent container security context to UID 1000,
the existing `node` UID already assumed by the pod `fsGroup` and workspace
ownership setup. Do not change the runner's permission mode or Studio task
detail page because neither is the source of the environment mismatch.

## Verification

Extend the Kubernetes pod-spec test first so it fails unless the main container
has `runAsUser: 1000`, while retaining the existing assertion that the init
container runs as root. Then run the targeted `server/agent` tests, the full Go
suite, and both production binary builds.
