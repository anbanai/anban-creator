import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentExecutionProfileCapability, BillingCatalog } from '@/types'
import { render } from '@/test/test-utils'
import { ExecutionProfileToolbar } from './ExecutionProfileToolbar'

const profiles: AgentExecutionProfileCapability[] = [
  {
    id: 'effective',
    display_name: '性价比',
    provider: 'deepseek',
    model_name: 'deepseek-v4-pro',
    description: '日常创作',
    min_tier: 'free',
    available: true,
  },
  {
    id: 'balanced',
    display_name: '平衡型',
    provider: 'volcengine_ark',
    model_name: 'doubao-seed-evolving',
    description: '质量与速度平衡',
    min_tier: 'pro',
    available: true,
  },
  {
    id: 'quality',
    display_name: '极致效果',
    provider: 'moonshot',
    model_name: 'kimi-k3[1m]',
    description: '复杂高质量创作',
    min_tier: 'enterprise',
    available: false,
    unavailable_reason: 'requires_enterprise',
  },
]

const catalog: BillingCatalog = {
  catalog_id: 'catalog-1',
  currency: 'credits',
  skus: [
    {
      id: 'article-effective',
      operation: 'task.article',
      charge_policy: 'task_admission',
      execution_profile: 'effective',
      price_credits: 4800,
      delivery: 'task',
    },
    {
      id: 'article-balanced',
      operation: 'task.article',
      charge_policy: 'task_admission',
      execution_profile: 'balanced',
      price_credits: 7200,
      delivery: 'task',
    },
  ],
}

describe('ExecutionProfileToolbar', () => {
  it('summarizes the selected priced profile and opens the full selector', async () => {
    const onChange = vi.fn()
    render(
      <ExecutionProfileToolbar
        profiles={profiles}
        value="effective"
        onChange={onChange}
        catalog={catalog}
        taskType="article"
      />,
    )

    const trigger = screen.getByRole('button', { name: /执行配置：性价比/ })
    expect(trigger).toHaveTextContent('性价比')
    expect(trigger).toHaveTextContent('4,800')

    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('button', { name: /^平衡型，/ }))

    expect(onChange).toHaveBeenCalledWith('balanced')
  })

  it('summarizes loading and honors the disabled state', () => {
    render(
      <ExecutionProfileToolbar
        profiles={[]}
        value=""
        onChange={vi.fn()}
        loading
        disabled
      />,
    )

    const trigger = screen.getByRole('button', { name: '执行配置：加载中' })
    expect(trigger).toHaveTextContent('加载中')
    expect(trigger).toBeDisabled()
  })

  it('omits a price when the catalog cannot resolve one', () => {
    render(
      <ExecutionProfileToolbar
        profiles={profiles}
        value="effective"
        onChange={vi.fn()}
        taskType="article"
      />,
    )

    const trigger = screen.getByRole('button', { name: '执行配置：性价比' })
    expect(trigger).toHaveTextContent('性价比')
    expect(trigger).not.toHaveTextContent('积分')
  })

  it('keeps unavailable profiles visible but unselectable in the full selector', async () => {
    render(
      <ExecutionProfileToolbar
        profiles={profiles}
        value="effective"
        onChange={vi.fn()}
        catalog={catalog}
        taskType="article"
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /执行配置：性价比/ }))
    const unavailable = await screen.findByRole('button', { name: /^极致效果，/ })
    expect(unavailable).toHaveTextContent('需要企业版')
    expect(unavailable).toBeDisabled()
  })

  it('updates the summary only after its controlled value changes', async () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <ExecutionProfileToolbar
        profiles={profiles}
        value="effective"
        onChange={onChange}
        catalog={catalog}
        taskType="article"
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /执行配置：性价比/ }))
    fireEvent.click(await screen.findByRole('button', { name: /^平衡型，/ }))
    expect(onChange).toHaveBeenCalledWith('balanced')
    expect(screen.getByRole('button', { name: /执行配置：性价比/ })).toBeInTheDocument()

    rerender(
      <ExecutionProfileToolbar
        profiles={profiles}
        value="balanced"
        onChange={onChange}
        catalog={catalog}
        taskType="article"
      />,
    )
    expect(screen.getByRole('button', { name: /执行配置：平衡型/ })).toHaveTextContent('7,200')
  })
})
