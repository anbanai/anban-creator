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
})
