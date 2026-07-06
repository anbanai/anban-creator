import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ReadinessChecklist } from './ReadinessChecklist'
import type { LocalExecutorStatus } from '@/lib/tauri'

function status(overrides: Partial<LocalExecutorStatus> = {}): LocalExecutorStatus {
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
    reason: '',
    ...overrides,
  }
}

describe('ReadinessChecklist', () => {
  it('renders structured desktop checks from local executor status', () => {
    render(
      <ReadinessChecklist
        status={status({
          checks: [
            { id: 'identity', label: 'Anban 身份', ok: true, required: true, hint: '平台密钥已配置' },
            { id: 'workspace', label: '本地工作区', ok: false, required: true, hint: '目录不可写' },
            { id: 'ffmpeg', label: 'ffmpeg', ok: false, required: false, hint: '可选' },
          ],
        })}
      />,
    )

    expect(screen.getByText('Anban 身份')).toBeInTheDocument()
    expect(screen.getByText('本地工作区')).toHaveAttribute('title', '目录不可写')
    expect(screen.getByText('ffmpeg')).toHaveAttribute('title', '可选')
  })

  it('falls back to legacy readiness fields when checks are absent', () => {
    render(
      <ReadinessChecklist
        status={status({
          api_key_set: true,
          claude_authenticated: true,
          workspace_set: true,
        })}
      />,
    )

    expect(screen.getByText('Anban Creator API Key')).toBeInTheDocument()
    expect(screen.getByText('Claude 鉴权')).toBeInTheDocument()
    expect(screen.getByText('本地工作区')).toBeInTheDocument()
  })
})
