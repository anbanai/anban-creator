import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { cpSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { HarnessClient } from '@deepseek-ai/dsh-sdk-client'
import { writeDshRouteSettings } from '../../dist/dsh-bridge.js'

const repo = fileURLToPath(new URL('../../../', import.meta.url))
const root = mkdtempSync(join(tmpdir(), 'anban-assistant-presets-'))
const staged = join(root, 'harness/dsh')
const presets = [
  { id: 'creation-planner', skill: 'creation-planning', context: 'CREATION_PLANNER_CONTEXT_MARKER' },
  { id: 'postmortem-review', skill: 'postmortem-review', context: 'POSTMORTEM_REVIEW_CONTEXT_MARKER' },
]
mkdirSync(staged, { recursive: true })
cpSync(join(repo, 'harness/dsh/plugins'), join(staged, 'plugins'), { recursive: true })
cpSync(join(repo, 'harness/dsh/lib'), join(staged, 'lib'), { recursive: true })
for (const preset of presets) {
  cpSync(join(repo, `harness/dsh/${preset.id}.patch.yml`), join(staged, `${preset.id}.patch.yml`))
  cpSync(join(repo, `harness/dsh/presets/${preset.id}/skills`), join(staged, `presets/${preset.id}/skills`), { recursive: true })
}
mkdirSync(join(root, 'runtime-dsh'))
symlinkSync(join(repo, 'runtime-dsh/node_modules'), join(root, 'runtime-dsh/node_modules'))

const pending = new Map()
let activePreset = ''
const server = createServer(async (req, res) => {
  if (req.method !== 'POST') { res.writeHead(405).end(); return }
  let body = ''
  for await (const chunk of req) body += chunk
  const request = JSON.parse(body)
  if (req.url === '/mcp') {
    if (request.id === undefined) { res.writeHead(202).end(); return }
    let result
    if (request.method === 'initialize') {
      result = { protocolVersion: request.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'smoke', version: '1' } }
    } else if (request.method === 'tools/list') {
      result = { tools: [
        { name: 'stage_assistant_draft', description: 'Stage an Assistant draft', inputSchema: { type: 'object', properties: {} } },
        { name: 'list_projects', description: 'Forbidden in Assistant mode', inputSchema: { type: 'object', properties: {} } },
        { name: 'forbidden_tool', description: 'Must be hidden', inputSchema: { type: 'object', properties: {} } },
      ] }
    } else if (request.method === 'ping') result = {}
    else {
      res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32601, message: 'Unsupported smoke method' } }))
      return
    }
    res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }))
    return
  }
  pending.get(activePreset)?.(request)
  res.writeHead(200, { 'Content-Type': 'text/event-stream' })
  const events = [
    ['message_start', { type: 'message_start', message: { id: 'smoke', type: 'message', role: 'assistant', model: 'smoke', content: [], stop_reason: null, stop_sequence: null, usage: { input_tokens: 1, output_tokens: 0 } } }],
    ['content_block_start', { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }],
    ['content_block_delta', { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'Ready' } }],
    ['content_block_stop', { type: 'content_block_stop', index: 0 }],
    ['message_delta', { type: 'message_delta', delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 1 } }],
    ['message_stop', { type: 'message_stop' }],
  ]
  for (const [event, data] of events) res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`)
  res.end()
})

await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const baseURL = `http://127.0.0.1:${server.address().port}`
const clients = []
let timer
try {
  for (const preset of presets) {
    const contextFile = join(root, `${preset.id}.context`)
    writeFileSync(contextFile, `${preset.context}\nASSISTANT_SESSION_ID=${preset.id}:smoke`)
    const home = join(root, `home-${preset.id}`)
    writeDshRouteSettings(home, { provider: 'anthropic', model: 'smoke', api: 'anthropic-messages', baseURL })
    const client = new HarnessClient({
      dshHome: home,
      profile: 'sdk',
      patches: [join(staged, `${preset.id}.patch.yml`)],
      processCwd: root,
      requestTimeoutMs: 15000,
      initializeTimeoutMs: 15000,
      env: {
        PATH: process.env.PATH,
        HOME: root,
        ANBAN_DSH_BRIDGE_MCP_URL: `${baseURL}/mcp`,
        ANBAN_DSH_BRIDGE_RUNTIME_TOKEN: 'smoke-token',
        ANBAN_DSH_BRIDGE_PROVIDER_API_KEY: 'smoke-key',
        ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE: contextFile,
      },
    })
    clients.push(client)
    let resolveRequest
    const requestPromise = new Promise(resolve => { resolveRequest = resolve })
    pending.set(preset.id, resolveRequest)
    activePreset = preset.id
    client.start()
    for (let attempt = 0; ; attempt += 1) {
      try {
        await client.initialize({ cwd: root, provider: 'anthropic', model: 'smoke' })
        break
      } catch (error) {
        if (attempt >= 20 || !error.message.includes('no adapter registered')) throw error
        await new Promise(resolve => setTimeout(resolve, 50))
      }
    }
    assert.deepEqual(await client.request('session/start', { sessionId: `${preset.id}:root`, presetId: preset.id }), { sessionId: `${preset.id}:root`, presetId: preset.id })
    const request = await Promise.race([requestPromise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`${preset.id} model request timed out`)), 10000) })])
    const toolNames = request.tools.map(tool => tool.name).sort()
    assert.deepEqual(toolNames, ['ask_user_question', 'mcp__creator__stage_assistant_draft', 'skill'], `${preset.id} tool allowlist`)
    const serialized = JSON.stringify(request)
    assert.ok(serialized.includes(preset.context), `${preset.id} Server context reaches first model request`)
    assert.ok(serialized.includes(preset.skill), `${preset.id} Skill reaches first model request`)
    assert.ok(!serialized.includes('mcp__creator__list_projects'), `${preset.id} project browsing stays unavailable`)
    assert.ok(!serialized.includes('forbidden_tool'), `${preset.id} unrelated tools stay unavailable`)
    console.log(`PASS: ${preset.id} preset, isolated tool allowlist, Skill and context injection`)
  }
} finally {
  clearTimeout(timer)
  await Promise.all(clients.map(client => client.close()))
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
  rmSync(root, { recursive: true, force: true })
}
