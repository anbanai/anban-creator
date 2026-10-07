import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { render } from '@/test/test-utils'
import { AgentIconStack } from './AgentIconStack'

describe('AgentIconStack', () => {
  it('shows multiple agent icons with accessible names and collapses overflow', () => {
    render(
      <AgentIconStack
        agentIds={['seednote', 'montage', 'hypit', 'wechat-article']}
        maxVisible={3}
      />,
    )

    expect(screen.getByLabelText('关联 Agent：种草笔记、视频生成、视频复刻、公众号文章')).toBeInTheDocument()
    expect(document.querySelectorAll('[data-slot="agent-icon"]')).toHaveLength(3)
    expect(document.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    expect(document.querySelector('[data-agent-id="montage"]')).toBeInTheDocument()
    expect(document.querySelector('[data-agent-id="hypit"]')).toBeInTheDocument()
    expect(screen.getByText('+1')).toHaveAttribute('title', '其余 Agent：公众号文章')
    expect(document.querySelector('[data-agent-id="seednote"]')).not.toHaveAttribute('tabindex')
  })

  it('uses the neutral empty state when a project has no associated agents', () => {
    render(<AgentIconStack agentIds={[]} />)

    expect(screen.getByText('尚未关联 Agent')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="agent-icon"]')).not.toBeInTheDocument()
  })
})
