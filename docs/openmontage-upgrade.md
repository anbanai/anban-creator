# OpenMontage Upgrade Procedure

OpenMontage is integrated as a git submodule at `third_party/OpenMontage`.
Anban owns the adapter contract and does not modify upstream OpenMontage source
files during normal feature work.

## Update

```bash
git submodule update --init --recursive
git -C third_party/OpenMontage fetch origin
git -C third_party/OpenMontage checkout origin/main
```

Review the submodule diff:

```bash
git diff --submodule=log
```

## Verify

```bash
go test ./server/agent -run OpenMontage -count=1
go test ./server/service -run OpenMontage -count=1
go test ./server/config -run OpenMontage -count=1
cd studio && bun run test -- src/lib/openmontage-form.test.ts src/pages/OpenMontageUx.contract.test.ts src/lib/schemas.test.ts
```

## Adapter Rule

If upstream pipeline metadata changes, update only Anban's OpenMontage adapter
mapping and tests. Do not copy OpenMontage internals into Studio schemas.

The stable Anban boundary remains:

- `openmontage_input` in API and Studio.
- `openmontage-input.json` in the agent workspace.
- `openmontage-project.json` as the adapter manifest.
- `final_video` plus `delivery-manifest.json` as required completion deliverables.
