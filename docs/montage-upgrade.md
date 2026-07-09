# Montage Upgrade Procedure

Montage is integrated as a git submodule at `third_party/OpenMontage`.
Anban owns the adapter contract and does not modify upstream Montage source
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
go test ./server/agent -run Montage -count=1
go test ./server/service -run Montage -count=1
go test ./server/config -run Montage -count=1
cd studio && bun run test -- src/lib/montage-form.test.ts src/pages/MontageUx.contract.test.ts src/lib/schemas.test.ts
```

## Adapter Rule

If upstream pipeline metadata changes, update only Anban's Montage adapter
mapping and tests. Do not copy Montage internals into Studio schemas.

The stable Anban boundary remains:

- `montage_input` in API and Studio.
- `montage-input.json` in the agent workspace.
- `montage-project.json` as the adapter manifest.
- `final_video` plus `delivery-manifest.json` as required completion deliverables.
