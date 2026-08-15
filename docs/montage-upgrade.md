# Montage Runtime Upgrade Procedure

OpenMontage is an external runtime dependency. It is not stored as a parent
repository submodule. Anban pins the original upstream repository and commit in
`deploy/docker/Dockerfile.agent-montage` and builds it directly into the Anban
Montage Agent image.

This packaging change does not change the managed runtime contract:

- The immutable template remains at `/opt/montage-template`.
- Each task receives a writable `/workspace/openmontage` copy.
- `ANBAN_MONTAGE_SUBMODULE_PATH` remains `/workspace/openmontage` for
  compatibility.
- The Agent Pack runtime adapter remains `openmontage`.
- Checkpoints, output links, task files, and resume behavior are unchanged.

## Local Build

The default source is the original upstream repository at the commit previously
recorded by the removed submodule:

```text
repository=https://github.com/calesthio/OpenMontage.git
commit=4eab34c5cfcccaa4f1970554928feccce73ee930
```

Build the Agent image:

```bash
make docker-montage-agent-image
```

The Dockerfile keeps the external source and dependency installation in stable
layers, so normal Docker layer caching still avoids rebuilding OpenMontage when
only the Anban Agent or plugin assets change.

## Yunxiao Build And Push

Use the repository root as the Docker build context. Configure one image-build
and push step in Yunxiao:

```bash
MONTAGE_AGENT_REF=chengdu.personal.cr.aliyuncs.com/bx_anbanai/creator-agent-montage:<release-tag>

docker build --pull \
  -f deploy/docker/Dockerfile.agent-montage \
  --build-arg OPENMONTAGE_REPO=https://github.com/calesthio/OpenMontage.git \
  --build-arg OPENMONTAGE_REF=4eab34c5cfcccaa4f1970554928feccce73ee930 \
  -t "$MONTAGE_AGENT_REF" .
docker push "$MONTAGE_AGENT_REF"
```

The Yunxiao worker needs outbound access to GitHub while building the image. It
does not need a checkout, fork, or submodule for OpenMontage.

## ACK Deployment

ACK only needs the final Montage Agent image. Set the existing
`montage_agent_image_repo` variable, which becomes
`ANBAN_AGENT_IMAGE_MONTAGE`, to the published `creator-agent-montage` reference.

Prefer an immutable digest in production:

```text
chengdu.personal.cr.aliyuncs.com/bx_anbanai/creator-agent-montage@sha256:<digest>
```

Existing executions continue using their persisted runtime image. New tasks use
the newly deployed Agent image after the Server deployment is updated.

## Upgrade OpenMontage

1. Choose an upstream commit from `https://github.com/calesthio/OpenMontage.git`.
2. Build and test the Montage Agent image using that full commit SHA.
3. Run the contract and managed-runtime verification below.
4. Publish the image with an immutable reference.
5. Update ACK to the new final Montage Agent digest.
6. Change the default `OPENMONTAGE_SOURCE_REF` only after acceptance succeeds.

Do not update OpenMontage implicitly from a branch such as `main`. A full commit
SHA keeps runtime builds reviewable and reproducible.

## Verification

```bash
go test ./server/agent -run Montage -count=1
go test ./server/service -run Montage -count=1
go test ./server/config -run Montage -count=1
(cd agent-ts && bun run typecheck && bun run test && bun run build)
(cd studio && bun run test -- src/lib/montage-form.test.ts src/pages/MontageUx.contract.test.ts src/lib/schemas.test.ts)
```

Verify a live deployment:

```bash
kubectl -n anbanai-prod get pod <pod> -o jsonpath='{range .status.initContainerStatuses[*]}{.name}{"="}{.imageID}{"\n"}{end}{range .status.containerStatuses[*]}{.name}{"="}{.imageID}{"\n"}{end}'
kubectl -n anbanai-prod get pod <pod> -o jsonpath='{range .spec.volumes[*]}{.name}{"="}{.persistentVolumeClaim.claimName}{"\n"}{end}'
kubectl -n anbanai-prod exec <pod> -- sh -c 'pwd; test -d /workspace/openmontage; test -w /workspace/openmontage; test -d /workspace/openmontage/remotion-composer; test -L /workspace/openmontage/output'
```

Backlot remains internal. The stable Anban boundary continues to be
`montage_input`, `montage-input.json`, `montage-project.json`, `final_video`, and
`delivery-manifest.json`.
