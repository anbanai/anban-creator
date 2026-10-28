# Auto-Editor Viral IP Video Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run pinned Auto-Editor audio-based dead-air detection in the dedicated `viral-ip-video-editor` runtime and pass reviewed cuts into the existing OpenMontage timeline.

**Architecture:** Package Auto-Editor 29.3.1 and the existing pinned OpenMontage runtime in an amd64, independent Pack-specific image. The Agent reads Auto-Editor's Premiere XML source in/out frames, records candidate and accepted source ranges plus cumulative composition-time mapping in its existing edit decision list, preserves original media, and leaves final rendering and all non-silence editing with OpenMontage.

**Tech Stack:** Go contract tests, Docker, Make, YAML/TOML/JSON Pack generation, Auto-Editor CLI, OpenMontage adapter.

**Spec:** `docs/superpowers/specs/2026-10-28-auto-editor-viral-ip-video-editor-design.md`

## Global Constraints

- Use `viral-ip-video-editor` consistently as the Pack id, runtime profile, image id, and runtime environment key suffix.
- Keep the original recording unchanged; final video rendering remains in OpenMontage.
- Audio threshold only proposes dead-air cuts; the Agent checks meaning, delivery, breaths, performance pauses, and cut continuity before accepting them.
- Generated Pack Agent files and catalogs come from the Pack source; do not edit generated Agent files by hand.
- When modifying distributed harness assets, update both native plugin versions and the package/changelog metadata together.

## Review Focus

- Auto-Editor must not make a GitHub request during task startup; the pinned binary must be fetched, executable, and version-checked during image build.
- The runtime image is amd64 because the pinned upstream launcher selects a Linux x86_64 binary.
- Premiere XML is candidate evidence only; OpenMontage receives the existing EDL/project format, including original source ranges and composition-time mapping.
- Rejected candidate intervals and adjustable pre/post margins must remain visible in the edit evidence so source time ranges and accepted cuts can be audited.
- The Pack-specific profile must resolve through local Compose, Server config, Kubernetes rendering, and release manifest generation.
- Missing executable or failed analysis must not be reported as successful automatic silence removal.

---

### Task 1: Pin and package Auto-Editor in the dedicated runtime image

**Files:**
- Create: `deploy/docker/Dockerfile.agent-viral-ip-video-editor`
- Modify: `server/agent/docker_runtime_contract_test.go`

**Interfaces:**
- Consumes: pinned OpenMontage repo/ref from the existing Montage image build.
- Produces: an amd64 one-shot OpenMontage-compatible runtime whose PATH includes `auto-editor` 29.3.1 and whose build fails if binary materialization or version verification fails.

- [x] Add a contract test that inventories the new Dockerfile and requires amd64, the OpenMontage pin, `auto-editor==29.3.1`, build-time `auto-editor --version` verification, plugin copy, and Montage environment contract.
- [x] Run the focused Go contract test and verify it fails because the image is absent.
- [x] Create the dedicated Dockerfile using the established Montage runtime, install the pinned Python launcher into the OpenMontage venv, and invoke it during the build to materialize the upstream binary.
- [x] Run the focused Go contract test and verify it passes.

### Task 2: Route dead-air removal through Auto-Editor

**Files:**
- Modify: `harness/agents/viral-ip-video-editor.md`
- Modify: `harness/packs/viral-ip-video-editor/agent-pack.yaml`
- Modify: harness package version, lock importer version, changelog, and Claude/Codex manifests
- Generate: Pack-local Agent surfaces and Server/Runtime Agent Pack catalogs

**Interfaces:**
- Consumes: Auto-Editor CLI in the Pack-specific image.
- Produces: reviewed silence-cut source ranges, Premiere XML evidence, and composition-time mapping in the existing `output/edit_decision_list.json` contract; OpenMontage still owns the final edit/render.

- [x] Update the existing Agent contract text to invoke Auto-Editor Premiere XML export for audio-threshold dead-air detection, translate XML source in/out frames into EDL source ranges and cumulative composition-time mapping, require transcript/performance/continuity review, retain adjustable margins, and record version/settings/candidate decisions in the EDL.
- [x] State the bounded failure/degradation behavior when the binary or analysis is unavailable.
- [x] Bump the Pack patch version, generate the Claude/Codex Agent surfaces and catalogs, update the plugin patch version/changelog, and run the Pack checks.

### Task 3: Register the runtime profile end to end

**Files:**
- Modify: `Makefile`
- Modify: `docker-compose.yml`
- Modify: `server/config.example.yaml`
- Modify: `server/Deployment.yaml`
- Modify: `scripts/render-ack-server-deployment.sh`
- Modify: `scripts/agent-runtime-release.sh`
- Modify: `server/config/runtime_images_test.go`
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `docs/deployment.md`

**Interfaces:**
- Consumes: `viral-ip-video-editor` Pack runtime profile and dedicated Dockerfile.
- Produces: `creator-agent-viral-ip-video-editor` image through development, release-manifest, Docker Compose, and Kubernetes paths.

- [x] Add failing contract assertions for the profile's Docker target/image variable and its Compose, Server, Kubernetes, and release-manifest mappings.
- [x] Run focused Go contract tests and verify they fail on the missing profile.
- [x] Add the independent amd64 image build target and map `ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR` to the Pack runtime profile throughout deployment and release tooling.
- [x] Run focused Go tests and release/config checks; verify all paths use the same profile/image identity.

### Task 4: Verify the complete integration

**Files:** all files from Tasks 1-3.

- [x] Run `make agent-pack-generate` and `make agent-pack-check` and confirm generated source/catalog parity.
- [x] Run `cd server && go test ./...` and build the Server binary.
- [x] Run the harness validation and package/plugin checks affected by the version bump.
- [x] Build `docker-viral-ip-video-editor-agent-image`; verify the materialized binary reports version 29.3.1 and a representative audio-threshold analysis generates Premiere XML.
- [x] Review `git diff` across both the parent repository and `harness` submodule, including unrelated pre-existing workspace state.
