import { describe, expect, it } from 'vitest'
import {
  canSubmitLocalTask,
  shouldDefaultRunLocally,
  localExecutorCreateHint,
} from './local-executor-ux'
import type { LocalExecutorStatus } from './tauri'

function status(overrides: Partial<LocalExecutorStatus>): LocalExecutorStatus {
  return {
    state: 'needs_config',
    available: false,
    running: false,
    agent_present: false,
    node_present: false,
    claude_present: false,
    plugin_present: false,
    ffmpeg_present: false,
    claude_authenticated: false,
    workspace_set: false,
    workspace_valid: false,
    workspace_writable: false,
    api_key_set: false,
    auth_mode: 'missing',
    checks: [],
    current_task_id: null,
    last_error: null,
    last_event_at: null,
    workspace: '',
    reason: '未配置',
    ...overrides,
  }
}

describe('local executor UX helpers', () => {
  it('defaults new tasks to local only when the executor loop can claim them now', () => {
    expect(shouldDefaultRunLocally(status({ state: 'running_idle', available: true, running: true }))).toBe(true)
    expect(shouldDefaultRunLocally(status({ state: 'claiming', available: true, running: true }))).toBe(true)
    expect(shouldDefaultRunLocally(status({ state: 'running_task', available: true, running: true }))).toBe(true)

    expect(shouldDefaultRunLocally(status({ state: 'ready_stopped', available: true, running: false }))).toBe(false)
    expect(shouldDefaultRunLocally(status({ state: 'needs_config', available: false, running: false }))).toBe(false)
    expect(shouldDefaultRunLocally(null)).toBe(false)
  })

  it('allows an explicit local submit only when the executor is running', () => {
    expect(canSubmitLocalTask(status({ state: 'running_idle', available: true, running: true }), true)).toBe(true)
    expect(canSubmitLocalTask(status({ state: 'ready_stopped', available: true, running: false }), true)).toBe(false)
    expect(canSubmitLocalTask(status({ state: 'needs_config', available: false, running: false }), true)).toBe(false)
    expect(canSubmitLocalTask(status({ state: 'running_idle', available: true, running: true }), false)).toBe(false)
  })

  it('explains local execution state in the create dialog', () => {
    expect(localExecutorCreateHint(status({ state: 'running_idle', available: true, running: true }))).toContain('可立即认领')
    expect(localExecutorCreateHint(status({ state: 'ready_stopped', available: true, running: false }))).toContain('点击启动')
    expect(localExecutorCreateHint(status({ state: 'needs_config', available: false, reason: '请选择本地工作区根目录' }))).toContain('请选择本地工作区根目录')
  })
})
