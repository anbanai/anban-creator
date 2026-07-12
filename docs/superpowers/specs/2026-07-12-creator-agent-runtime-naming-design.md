# Creator Agent Runtime Naming Design

## Goal

Rename the Agent container and its deployment-facing runtime identity from
`anban-agent` to `creator-agent`, keeping related Kubernetes, image, test, and
documentation names consistent.

## Scope

Update runtime-visible names that identify the same Agent workload:

- Kubernetes Pod name prefix;
- `app.kubernetes.io/name` label value;
- Agent container name;
- image names and examples that currently use `anban-agent`;
- Compose container naming where it represents the Agent runtime;
- deployment and runtime contract tests;
- current configuration examples, plans, and design documentation containing
  the old runtime identity.

After implementation, active repository code, configuration, tests, and docs
must not retain `anban-agent` as an Agent runtime identity.

## Stable Interfaces

Keep generic Agent configuration and implementation interfaces unchanged:

- `ANBAN_AGENT_*` environment variables;
- `agent_image` YAML fields;
- `Dockerfile.agent`;
- `docker-agent-image` Make target;
- Go types, packages, and functions whose `Agent` name describes the domain.

These interfaces describe Agent behavior or configuration rather than the
container's deployed identity. Renaming them would create unnecessary operator
and code compatibility changes outside the requested container rename.

## Implementation

Define `creator-agent` as the single Kubernetes runtime identity used by Pod
names, workload labels, and the primary Agent container. Update image examples
and deployment fixtures to use the same name. Preserve existing hashing and
length-limiting behavior for generated Kubernetes resource names.

Historical design and plan examples that still prescribe `anban-agent` will be
updated because they remain searchable operational guidance in this repository.
The documents' architecture and decisions otherwise remain unchanged.

## Verification

Use a red-green contract update:

1. Change focused Kubernetes naming tests to expect `creator-agent` and confirm
   they fail against the old implementation.
2. Update the runtime implementation and related fixtures until focused tests
   pass.
3. Search the repository for unintended `anban-agent` remnants.
4. Run the affected Go package tests, the full Go test suite, and Server and
   Agent builds.

The unrelated dirty `third_party/OpenMontage` submodule state is outside this
change and must remain untouched.
