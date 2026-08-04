import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentExecutionProfileCapability, BillingCatalog } from '@/types'
import { render } from '@/test/test-utils'
import { TaskComposerParameters } from './TaskComposerParameters'

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
  skus: [{
    id: 'article-effective',
    operation: 'task.article',
    charge_policy: 'task_admission',
    execution_profile: 'effective',
    price_credits: 4800,
    delivery: 'task',
  }],
}

describe('TaskComposerParameters', () => {
  it('combines execution, image, and finite task quantity settings in a wide compact popover', () => {
    const onExecutionProfileChange = vi.fn()
    const onRatioChange = vi.fn()
    const onCapabilityChange = vi.fn()
    const onQuantityChange = vi.fn()

    render(
      <TaskComposerParameters
        execution={{
          profiles,
          value: 'effective',
          onChange: onExecutionProfileChange,
          catalog,
          taskType: 'article',
        }}
        image={{
          ratios: ['16:9', '4:3'],
          ratio: '16:9',
          onRatioChange,
          capabilities: [{
            key: 'standard',
            display_name: '标准图像',
            description: '适合日常配图',
            enabled: true,
            price_available: true,
            price_credits: 300,
          }],
          capabilityKey: 'standard',
          onCapabilityChange,
        }}
        quantity={{
          label: '任务数量',
          value: 1,
          min: 1,
          max: 5,
          onChange: onQuantityChange,
        }}
      />,
    )

    const trigger = screen.getByRole('button', { name: /创作参数：/ })
    expect(trigger).toHaveTextContent('创作参数')
    expect(screen.queryByRole('button', { name: /^执行配置：/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^图像设置：/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '任务数量：1' })).not.toBeInTheDocument()

    fireEvent.click(trigger)
    const popover = screen.getByRole('dialog', { name: '创作参数' })
    expect(within(popover).getByText('执行配置')).toBeInTheDocument()
    expect(popover).toHaveClass('w-[min(48rem,calc(100vw-1rem))]', 'gap-1.5', 'p-2')
    expect(within(popover).getByRole('group', { name: 'Agent 执行配置' })).toHaveClass('grid-cols-3')
    expect(within(popover).getByRole('group', { name: 'Agent 执行配置' })).not.toHaveClass('grid-cols-1')
    expect(within(popover).getByRole('group', { name: '图片比例' })).toBeInTheDocument()
    expect(within(popover).getByRole('group', { name: '图像能力' })).toBeInTheDocument()
    expect(within(popover).getByLabelText('任务数量：1')).toBeInTheDocument()
    expect(within(popover).getByText('任务数量').closest('section')).toHaveClass(
      'grid-cols-[4.5rem_minmax(0,1fr)]',
    )

    fireEvent.click(within(popover).getByRole('button', { name: /^平衡型，/ }))
    fireEvent.click(within(popover).getByRole('button', { name: '4:3' }))
    fireEvent.click(within(popover).getByRole('button', { name: '增加任务数量' }))
    expect(onExecutionProfileChange).toHaveBeenCalledWith('balanced')
    expect(onRatioChange).toHaveBeenCalledWith('4:3')
    expect(onQuantityChange).toHaveBeenCalledWith(2)
  })

  it('omits image and quantity sections when they do not apply', () => {
    render(
      <TaskComposerParameters
        execution={{ profiles, value: 'effective', onChange: vi.fn() }}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /创作参数：/ }))
    const popover = screen.getByRole('dialog', { name: '创作参数' })
    expect(within(popover).getByText('执行配置')).toBeInTheDocument()
    expect(within(popover).queryByText('图片比例')).not.toBeInTheDocument()
    expect(within(popover).queryByText('任务数量')).not.toBeInTheDocument()
  })

  it('lets a fixed task quantity use the full compact row', () => {
    render(
      <TaskComposerParameters
        execution={{ profiles, value: 'effective', onChange: vi.fn() }}
        quantity={{
          label: '任务数量',
          value: 1,
          min: 1,
          max: 1,
          onChange: vi.fn(),
        }}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /创作参数：/ }))
    const fixedQuantity = screen.getByText('任务数量 1 · 当前能力上限')
    expect(fixedQuantity.parentElement).toHaveClass('col-span-2')
  })
})
