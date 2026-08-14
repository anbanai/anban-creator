# Seednote Server MCP External Research Design

## Problem

Managed Seednote executions install Agent Reach, Python, and mcporter in every
runtime image, then ask Agent Reach to select an external Xiaohongshu backend.
That indirection does not work in ACK: Agent Reach probes
`http://localhost:18060/mcp`, while the independently deployed
`xiaohongshu-mcp` workload is available at
`http://sidecar-seednote:18060/mcp`. The one-shot Agent Job does not receive a matching
mcporter configuration.

Agent Reach is primarily an installer, environment doctor, credential helper,
and selector for multiple third-party command families. Its own MCP surface
only reports status. It does not proxy Xiaohongshu search or detail requests.
The Anban Server already exposes authenticated MCP tools for the Xiaohongshu
operations required by managed Seednote research, so retaining Agent Reach in
the managed runtime adds dependencies without owning the data path.

## Product Boundary

Managed Seednote external research supports Xiaohongshu through the Anban
Server MCP. It does not promise Agent Reach's broader Web, YouTube, Twitter,
Reddit, RSS, Bilibili, LinkedIn, V2EX, Xueqiu, or transcription capabilities.

Original Seednote tasks treat external research as an optional enhancement. If
the capability is unavailable or logged out, they continue from the explicit
topic, topic pool, project profile, and title history without presenting local
judgment as external trend evidence. Replicate tasks that only contain an
external note identifier or URL fail recoverably when the source note cannot be
resolved.

## Architecture

Keep the existing three-process boundary:

```text
Managed Seednote Agent Job
  -> authenticated Anban Server MCP
  -> SeednoteCapabilityService
  -> xiaohongshu-mcp REST API at http://sidecar-seednote:18060
  -> Xiaohongshu
```

The Agent Job never connects directly to the unauthenticated
`xiaohongshu-mcp` ClusterIP service. The Server remains the authenticated
capability boundary and owns readiness, request timeouts, response shaping,
and structured errors. The independent `sidecar-seednote` Deployment and its
`sidecar-seednote-data` PVC remain unchanged.

No new MCP server, MCP proxy, backend router, database schema, Studio surface,
or task field is introduced.

## MCP Contract

Promote the existing Server MCP tools from legacy fallback to the canonical
managed Seednote external-data interface:

- `check_seednote_login_status`
- `get_seednote_login_qrcode`
- `search_seednote_feeds`
- `get_seednote_feed_detail`
- `get_seednote_user_profile`

Each handler continues to invoke exactly one application-service capability.
Workflow sequencing, retries, fallback decisions, provenance recording, and
quality gates remain in the Seednote Agent and Skills.

Managed research is read-only. Publishing, deleting, following, liking,
collecting, and commenting are outside this contract. Search results remain
the authoritative source of `feed_id` and `xsec_token`; Agents must not invent
either value.

## Agent Workflow

For research requiring Xiaohongshu data, the Agent first calls
`check_seednote_login_status`.

- `available=true` and `logged_in=true`: call the required search, detail, or
  user-profile tool.
- `available=false`: record the returned availability message and use the
  original-mode fallback or replicate-mode recoverable failure rule.
- `available=true` and `logged_in=false`: report that login is required. The
  QR-code tool is an operator-facing recovery capability, not an automatic
  login workflow.
- A transient tool failure may be retried once by the Skill. Persistent
  failures follow the same fallback or recoverable-failure split.

Research artifacts record the concrete tool used, `data_source=xiaohongshu-mcp`,
availability, login state, token source, missing fields, and fallback reason.
They no longer record an Agent Reach status or selected backend.

## Dependency Removal

Remove Agent Reach from the managed Seednote execution and repository build
contract:

- remove `third_party/Agent-Reach` and its `.gitmodules` entry;
- remove the Agent Reach update script and Make target;
- remove Agent Reach, Python, Python venv, and mcporter installation from both
  Seednote runtime Dockerfiles;
- remove the Agent Reach runtime path constants and policy capability marker;
- remove Agent Reach-specific Docker, submodule, Skill, and workflow contract
  tests;
- update Seednote Agents and Skills to call the canonical Anban MCP tools;
- remove the distributed `agent-reach` Skill and its references;
- bump both native plugin manifest versions together, following the plugin
  release contract.

Python remains in Montage images because OpenMontage requires it. Article and
Seednote images must not acquire unrelated Python dependencies after this
change.

## Deployment

Deploy `deploy/k8s/ack-sidecar-ilink.yaml` and `deploy/k8s/ack-sidecar-seednote.yaml` as two
independent Yunxiao workflows with an immutable
`xpzouying/xiaohongshu-mcp@sha256:...` image reference for Seednote. The Server
and each sidecar use the same namespace, and the Server retains
`ANBAN_SEEDNOTE_BASE_URL=http://sidecar-seednote:18060`.

No Agent Job environment variable or mcporter configuration is needed. The
sidecar remains a single-replica `Recreate` Deployment because browser and
login state are persisted on one ReadWriteOnce PVC.

## Failure Handling And Security

- An unavailable sidecar returns a structured `available=false` result where
  the existing capability contract supports it.
- Invalid tool inputs return stable validation errors without calling the
  sidecar.
- Sidecar transport and protocol errors remain tool failures with their
  operation context preserved.
- Secrets and cookies never enter task artifacts, Agent prompts, tool results,
  or logs.
- The sidecar stays `ClusterIP` only. No Ingress, LoadBalancer, or NodePort is
  added.
- Managed Agent Jobs use the authenticated Server MCP and do not receive direct
  sidecar credentials or network configuration.

## Tests

1. Server MCP tests cover login status, search, detail, user profile,
   unavailable readiness, validation errors, and shaped read-only responses.
2. Seednote Agent and Skill contract tests require the canonical Server MCP
   tools and reject Agent Reach, mcporter, OpenCLI, and direct sidecar calls.
3. Docker contract tests reject Agent Reach, Python, venv, and mcporter from
   both Seednote images while retaining the plugin and managed runner.
4. Repository tests reject a stale Agent Reach submodule, update target, or
   runtime-path reference.
5. ACK sidecar tests continue to require the immutable image reference, PVC,
   health probes, one replica, `Recreate`, and ClusterIP-only exposure.
6. Run full Go tests and build both Server and Agent binaries. Build and smoke
   test the selected Seednote runtime image. Plugin contract changes also run
   the targeted `server/agent` and `server/mcp` tests.

## Release And Acceptance

Publish the plugin child commit before updating the parent gitlink. Build and
publish the selected Seednote Agent image with an immutable digest, update
`ANBAN_AGENT_IMAGE_SEEDNOTE`, and roll the Server only if its configured image
mapping or Server code changes. The existing `xiaohongshu-mcp` digest can stay
unchanged when it already passes health and login checks.

Use a fresh managed Seednote task for acceptance. It must:

1. start with the new Seednote image digest;
2. call `check_seednote_login_status` through the authenticated Server MCP;
3. call `search_seednote_feeds` and `get_seednote_feed_detail` when logged in;
4. preserve a real `feed_id` and `xsec_token` from tool output;
5. write provenance without Agent Reach fields;
6. complete original research or produce the specified recoverable replicate
   failure when external source retrieval is unavailable;
7. contain no `agent-reach`, `mcporter`, or Python dependency in the running
   Seednote image.
