import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { mkdtempSync, mkdirSync, cpSync, symlinkSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { HarnessClient } from '@deepseek-ai/dsh-sdk-client'
import { assertInteractiveRuntimeCapabilities, writeDshRouteSettings } from '../../dist/dsh-bridge.js'

const repo = fileURLToPath(new URL('../../../', import.meta.url))
const root = mkdtempSync(join(tmpdir(), 'anban-creator-layout-'))
const staged = join(root, 'harness/dsh')
mkdirSync(staged, { recursive: true })
cpSync(join(repo, 'harness/dsh/project-creator.patch.yml'), join(staged, 'project-creator.patch.yml'))
cpSync(join(repo, 'harness/dsh/plugins'), join(staged, 'plugins'), { recursive: true })
cpSync(join(repo, 'runtime-dsh/dist/harness-dsh/lib'), join(staged, 'lib'), { recursive: true })
cpSync(join(repo, 'harness/dsh/presets/project-creator/skills'), join(staged, 'presets/project-creator/skills'), { recursive: true })
mkdirSync(join(root, 'runtime-dsh'))
symlinkSync(join(repo, 'runtime-dsh/node_modules'), join(root, 'runtime-dsh/node_modules'))
symlinkSync(join(repo, 'runtime-dsh/node_modules'), join(root, 'node_modules'))
const snapshot = 'PROJECT_CREATOR_SNAPSHOT_SMOKE_MARKER'
writeFileSync(join(root, 'context.json'), snapshot)
let resolveModel
const modelRequest = new Promise(resolve => { resolveModel = resolve })
const server = createServer(async (req, res) => {
  if (req.method !== 'POST') { res.writeHead(405).end(); return }
  let body = ''
  for await (const chunk of req) body += chunk
  const data = JSON.parse(body)
  if (req.url === '/mcp') {
    if (data.id === undefined) { res.writeHead(202).end(); return }
    let result
    if (data.method === 'initialize') result = { protocolVersion: data.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'smoke', version: '1' } }
    else if (data.method === 'tools/list') result = { tools: [
      { name: 'stage_project_creator_draft', description: 'Stage draft', inputSchema: { type: 'object', properties: {} } },
      ...['list_projects', 'get_project', 'get_project_profile', 'list_project_titles', 'list_drafts', 'list_published_articles', 'list_topics', 'list_trends', 'list_resources', 'get_resource'].map(name => ({ name, description: name, inputSchema: { type: 'object', properties: {} } })),
      { name: 'forbidden_tool', description: 'Must be hidden', inputSchema: { type: 'object', properties: {} } },
    ] }
    else if (data.method === 'ping') result = {}
    else { res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: data.id, error: { code: -32601, message: 'Unsupported smoke method' } })); return }
    res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: data.id, result }))
    return
  }
  resolveModel(data)
  res.writeHead(200, { 'Content-Type': 'text/event-stream' })
  const events = [
    ['message_start', { type: 'message_start', message: { id: 'smoke', type: 'message', role: 'assistant', model: 'smoke', content: [], stop_reason: null, stop_sequence: null, usage: { input_tokens: 1, output_tokens: 0 } } }],
    ['content_block_start', { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }],
    ['content_block_delta', { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'Ready' } }],
    ['content_block_stop', { type: 'content_block_stop', index: 0 }],
    ['message_delta', { type: 'message_delta', delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 1 } }],
    ['message_stop', { type: 'message_stop' }],
  ]
  for (const [event, data] of events) res.write('event: ' + event + '\ndata: ' + JSON.stringify(data) + '\n\n')
  res.end()
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const baseURL = 'http://127.0.0.1:' + server.address().port
const home = join(root, 'home')
writeDshRouteSettings(home, { provider: 'anthropic', model: 'smoke', api: 'anthropic-messages', baseURL })
const client = new HarnessClient({
  dshHome: home, profile: 'sdk', patches: [join(staged, 'project-creator.patch.yml')], processCwd: root,
  requestTimeoutMs: 15000, initializeTimeoutMs: 15000,
  env: { PATH: process.env.PATH, HOME: root, ANBAN_DSH_BRIDGE_MCP_URL: baseURL + '/mcp', ANBAN_DSH_BRIDGE_RUNTIME_TOKEN: 'smoke-token', ANBAN_DSH_BRIDGE_PROVIDER_API_KEY: 'smoke-key', ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE: join(root, 'context.json') },
})
let timer
try {
  client.start()
  for (let attempt = 0; ; attempt++) {
    try {
      const initialized = await client.initialize({ cwd: root, provider: 'anthropic', model: 'smoke' })
      assertInteractiveRuntimeCapabilities(initialized)
      break
    }
    catch (error) {
      if (attempt >= 20 || !error.message.includes('no adapter registered')) throw error
      await new Promise(resolve => setTimeout(resolve, 50))
    }
  }
  assert.deepEqual(await client.request('session/start', { sessionId: 'smoke-root', presetId: 'project-creator' }), { sessionId: 'smoke-root', presetId: 'project-creator' })
  const request = await Promise.race([modelRequest, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('model request timed out')), 10000) })])
  assert.deepEqual(request.tools.map(tool => tool.name).sort(), [
    'ask_user_question',
    'mcp__creator__get_project',
    'mcp__creator__get_project_profile',
    'mcp__creator__get_resource',
    'mcp__creator__list_drafts',
    'mcp__creator__list_project_titles',
    'mcp__creator__list_projects',
    'mcp__creator__list_published_articles',
    'mcp__creator__list_resources',
    'mcp__creator__list_topics',
    'mcp__creator__list_trends',
    'mcp__creator__stage_project_creator_draft',
    'skill',
  ])
  assert.ok(JSON.stringify(request.messages).includes(snapshot), 'runtime snapshot reaches first model request')
  assert.ok(JSON.stringify(request).includes('project-creator-client-discovery'), 'Project Creator discovery Skill reaches first model request')
  const serializedRequest = JSON.stringify(request)
  assert.ok(serializedRequest.includes('你要先弄清客户的业务及内容要解决的问题'), 'updated Project Creator persona reaches first model request')
  assert.ok(!serializedRequest.includes('优先确认项目名称/一句话定位、主平台和内容类型'), 'obsolete fixed interview priority is absent from first model request')
  console.log('PASS: deployed layout session/start, real tool allowlist, and context injection')
} finally {
  clearTimeout(timer)
  await client.close()
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
  rmSync(root, { recursive: true, force: true })
}
