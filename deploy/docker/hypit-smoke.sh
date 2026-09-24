#!/usr/bin/env bash
# Local rendering only: no hosted generation account is used.
set -euo pipefail
image="${1:-creator-agent-hypit:latest}"
revision="${2:-5a568f4be485ab5e735fe95533cd5f77a85c66ee}"
docker info >/dev/null
docker run --rm -i --platform linux/amd64 --init --cpus 4 --memory 8g \
  --read-only --network none -e HOME=/workspace/.anban-runtime-home \
  --tmpfs /tmp:rw,mode=1777,size=1g \
  --tmpfs /workspace:rw,exec,uid=1000,gid=1000,mode=0750,size=4g \
  --entrypoint /bin/bash "$image" -s -- "$revision" <<'CONTAINER'
set -euo pipefail
[[ "$(id -u)" == 1000 ]]
[[ "$(cat /opt/hypit/.anban-source-revision)" == "$1" ]]
test ! -w /opt/hypit/package.json
test -s /opt/hypit/skills/hypit/SKILL.md
test -s /opt/hypit/LICENSE
(cd /opt/hypit && sha256sum --quiet -c .anban-source-files.sha256)
node --input-type=module <<'JS'
import { prepareHypitToolHome } from '/app/anban-agent/dist/hypit.js';
await prepareHypitToolHome('/workspace', process.env);
JS
hypit --version
ffmpeg -version >/dev/null
ffprobe -version >/dev/null
cp -R /opt/hypit/examples/semantic-composition /workspace/project
chmod -R u+w /workspace/project
cd /workspace/project
cat > package.json <<'JSON'
{"name":"video-runtime-smoke","private":true,"type":"module","dependencies":{"@hypit/hypit":"file:/opt/hypit","@example/chat-scene":"file:./packages/chat-scene"}}
JSON
mkdir -p node_modules/@hypit node_modules/@example
ln -s /opt/hypit node_modules/@hypit/hypit
ln -s ../../packages/chat-scene node_modules/@example/chat-scene
/opt/hypit/node_modules/.bin/tsc -p packages/chat-scene/tsconfig.json
# The copied example belongs to upstream's monorepo. Its compiled component is
# now a standalone project package, whose development dependencies stay upstream.
node --input-type=module <<'JS'
import { readFileSync, writeFileSync } from 'node:fs';
const file = 'packages/chat-scene/package.json';
const manifest = JSON.parse(readFileSync(file, 'utf8'));
delete manifest.devDependencies;
writeFileSync(file, JSON.stringify(manifest, null, 2));
JS
cat > /workspace/runtime-profile.json <<'JSON'
{"format":"hypit.runtime-local@1","dataRoot":"/workspace/project/.hypit/execution","credentials":{},"endpoints":{"media.local":{"use":"@hypit/provider-media-local"},"hyperframes.local":{"use":"@hypit/provider-hyperframes-local","config":{"workers":2,"defaultConcurrency":1,"browserGpu":"software"}}},"bindings":{}}
JSON
trap 'hypit runtime down --runtime /workspace/runtime-profile.json >/dev/null 2>&1 || true' EXIT
hypit check chat.svrun
hypit plan chat.svrun --runtime /workspace/runtime-profile.json
hypit build chat.svrun --runtime /workspace/runtime-profile.json --follow --title runtime-smoke
build_id="$(hypit builds --json | jq -er '.builds[0].id')"
hypit get "$build_id" --output final.video --to /workspace/final.mp4
ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of json /workspace/final.mp4 | jq -e '.streams[0].width == 540 and .streams[0].height == 960' >/dev/null
ffmpeg -nostdin -v error -xerror -i /workspace/final.mp4 -f null -
hypit runtime down --runtime /workspace/runtime-profile.json
mkdir -p productions/main/runs
cat > productions/main/runs/main.svrun <<'RUN'
<?svml using="@hypit/run-markup@1"?>
<svrun version="1"><author source="../../../chat.svml"/><target output="final.video"/></svrun>
RUN
node --input-type=module <<'JS'
import { mkdir, rename, writeFile } from 'node:fs/promises';
import { exportHypitProject, prepareHypitWorkspace, validateHypitReferences } from '/app/anban-agent/dist/hypit.js';
await validateHypitReferences('/workspace/project');
await mkdir('/workspace/.anban-creator', { recursive: true });
await exportHypitProject('/workspace/project', '/workspace/.anban-creator/project.zip');
await rename('/workspace/project', '/workspace/original-project');
await writeFile('/workspace/input.json', JSON.stringify({ project_archive_path: '.anban-creator/project.zip' }));
await prepareHypitWorkspace('/workspace', { ...process.env });
JS
printf 'PASS: official source, non-root rendering, full decode and archived project restore/check/plan\n'
CONTAINER
