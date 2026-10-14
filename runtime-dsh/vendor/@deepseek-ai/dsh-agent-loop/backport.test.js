import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Context } from '@deepseek-ai/cordis'
import LlmRuntime, { LlmAdapter, createUserMessage } from '@deepseek-ai/dsh-llm'
import SessionStore, { SessionId } from '@deepseek-ai/dsh-session'
import SessionProjectionRegistry from '@deepseek-ai/dsh-session-projection'
import SystemPrompt from '@deepseek-ai/dsh-system-prompt'
import ToolRuntime from '@deepseek-ai/dsh-tools'
import AgentRegistry from '@deepseek-ai/dsh-agent'

const { default: AgentLoop } = await import(process.env.DSH_BACKPORT_BASELINE === '1'
  ? '@deepseek-ai/dsh-agent-loop' : './lib/index.js')

class Adapter extends LlmAdapter {
  requests = []
  entered = Promise.withResolvers()
  constructor(hang = false) { super(); this.hang = hang }
  async resolveModel(provider, model) { return { provider, id: model, name: model } }
  async *stream(options) {
    this.requests.push(options)
    this.entered.resolve()
    if (this.hang) {
      await new Promise((resolve, reject) => {
        const abort = () => reject(new Error('aborted'))
        if (options.signal.aborted) abort()
        else options.signal.addEventListener('abort', abort, { once: true })
      })
      return
    }
    yield { type: 'block-start', index: 0, blockType: 'text' }
    yield { type: 'text-delta', index: 0, text: 'welcome' }
    yield { type: 'block-end', index: 0, block: { type: 'text', text: 'welcome' } }
    yield { type: 'finish', reason: { kind: 'stop' } }
  }
}

async function setup(t, hang = false) {
  const ctx = new Context()
  t.after(() => ctx.fiber.dispose())
  for (const plugin of [LlmRuntime, SessionStore, SessionProjectionRegistry, SystemPrompt, ToolRuntime, AgentRegistry]) {
    await ctx.plugin(plugin)
  }
  await ctx.plugin(AgentLoop, { agents: [] })
  const adapter = new Adapter(hang)
  ctx.llm.registerAdapter(['mock'], adapter)
  const agent = await ctx.agentLoop.create(SessionId('backport-test'), { provider: 'mock', model: 'mock' })
  assert.equal(typeof agent.start, 'function', 'published loop must expose message-free start')
  return { agent, adapter }
}

test('start opens exactly one model turn without a user transcript row', { timeout: 5000 }, async t => {
  const { agent, adapter } = await setup(t)
  agent.start()
  agent.start()
  await agent.whenIdle()
  agent.start()
  await agent.whenIdle()
  assert.equal(adapter.requests.length, 1)
  assert.equal(adapter.requests[0].messages.some(message => message.role === 'user'), false)
  const events = agent.session.snapshotEvents()
  assert.equal(events.filter(event => event.type === 'turn/start').length, 1)
  assert.equal(events.filter(event => event.type === 'user/message').length, 0)
})

test('cancelled startup is not retried by repeated start and a later followup still works', { timeout: 5000 }, async t => {
  const { agent, adapter } = await setup(t, true)
  agent.start()
  await adapter.entered.promise
  agent.cancel({ kind: 'user' })
  await agent.whenIdle()
  agent.start()
  await agent.whenIdle()
  assert.equal(adapter.requests.length, 1)
  assert.deepEqual(agent.session.snapshotEvents().find(event => event.type === 'turn/end').data.reason,
    { kind: 'aborted', reason: { kind: 'user' } })
  adapter.hang = false
  agent.followup(createUserMessage({ content: [{ type: 'text', text: 'continue' }], source: { kind: 'user' } }))
  await agent.whenIdle()
  assert.equal(adapter.requests.length, 2)
})

test('cancelling before model dispatch does not let a later start replay startup', { timeout: 5000 }, async t => {
  const { agent, adapter } = await setup(t)
  agent.start()
  agent.cancel({ kind: 'user' })
  await agent.whenIdle()
  agent.start()
  await agent.whenIdle()
  assert.equal(adapter.requests.length, 0)
  assert.equal(agent.session.snapshotEvents().filter(event => event.type === 'user/message').length, 0)
})
