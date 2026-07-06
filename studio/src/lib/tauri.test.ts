import { afterEach, describe, expect, it, vi } from 'vitest'
import { setLocalExecutorConfig } from './tauri'

describe('tauri desktop bridge', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    delete window.__TAURI__
  })

  it('sends local executor config as a patch so blank secret fields are preserved', async () => {
    const invoke = vi.fn().mockResolvedValue(true)
    window.__TAURI__ = { core: { invoke } }

    const ok = await setLocalExecutorConfig({
      apiKey: '',
      workspace: '/Users/me/anban-tasks',
      claudeApiKey: '',
    })

    expect(ok).toBe(true)
    expect(invoke).toHaveBeenCalledWith('set_local_executor_config', {
      apiKey: '',
      workspace: '/Users/me/anban-tasks',
      claudeApiKey: '',
      clearApiKey: false,
      clearClaudeApiKey: false,
    })
  })

  it('can explicitly request secret clearing', async () => {
    const invoke = vi.fn().mockResolvedValue(true)
    window.__TAURI__ = { core: { invoke } }

    await setLocalExecutorConfig({
      apiKey: '',
      workspace: '',
      claudeApiKey: '',
      clearApiKey: true,
      clearClaudeApiKey: true,
    })

    expect(invoke).toHaveBeenCalledWith('set_local_executor_config', {
      apiKey: '',
      workspace: '',
      claudeApiKey: '',
      clearApiKey: true,
      clearClaudeApiKey: true,
    })
  })
})
