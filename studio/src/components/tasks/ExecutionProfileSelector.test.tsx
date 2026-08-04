import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ExecutionProfileSelector } from './ExecutionProfileSelector'

const profiles = [
  { id: 'effective' as const, display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-pro', description: '日常创作', min_tier: 'free' as const, available: true },
  { id: 'balanced' as const, display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro' as const, available: true },
  { id: 'quality' as const, display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise' as const, available: false, unavailable_reason: 'requires_enterprise' },
]

describe('ExecutionProfileSelector', () => {
  it('shows every public execution profile without internal routing details', () => {
    render(<ExecutionProfileSelector profiles={profiles} value="effective" onChange={() => {}} />)

    expect(screen.getByText('性价比')).toBeInTheDocument()
    expect(screen.getByText('平衡型')).toBeInTheDocument()
    expect(screen.getByText('极致效果')).toBeInTheDocument()
    expect(screen.queryByText('deepseek')).not.toBeInTheDocument()
    expect(screen.queryByText('volcengine_ark')).not.toBeInTheDocument()
    expect(screen.queryByText('moonshot')).not.toBeInTheDocument()
    expect(screen.queryByText('doubao-seed-evolving')).not.toBeInTheDocument()
    expect(screen.queryByText('kimi-k3[1m]')).not.toBeInTheDocument()
    expect(screen.getByText('全部用户')).toBeInTheDocument()
    expect(screen.getByText('Pro 版及以上')).toBeInTheDocument()
    expect(screen.getByText('企业版')).toBeInTheDocument()
    expect(screen.getByText('需要企业版')).toBeInTheDocument()
    expect(screen.queryByText('requires_enterprise')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /极致效果/ })).toBeDisabled()
    expect(screen.getByRole('group', { name: 'Agent 执行配置' })).toHaveClass('grid-cols-1')
    expect(screen.getByRole('group', { name: 'Agent 执行配置' })).not.toHaveClass('sm:grid-cols-3')
    expect(screen.getByRole('button', { name: /^性价比，/ })).toHaveClass('min-h-16')
    expect(screen.getByRole('button', { name: /^性价比，/ })).not.toHaveClass('min-h-24')
  })

  it('reports an available profile selection without clearing the current value', () => {
    const onChange = vi.fn()
    function Harness() {
      const [value, setValue] = useState<'effective' | 'balanced' | 'quality'>('effective')
      return <ExecutionProfileSelector profiles={profiles} value={value} onChange={(next) => { setValue(next); onChange(next) }} />
    }
    render(<Harness />)

    fireEvent.click(screen.getByRole('button', { name: /^平衡型，/ }))
    expect(onChange).toHaveBeenCalledWith('balanced')

    fireEvent.click(screen.getByRole('button', { name: /^性价比，/ }))
    expect(onChange).toHaveBeenLastCalledWith('effective')
  })

  it('supports a compact horizontal layout for composer popovers', () => {
    render(
      <ExecutionProfileSelector
        profiles={profiles}
        value="effective"
        onChange={() => {}}
        layout="horizontal"
      />,
    )

    const group = screen.getByRole('group', { name: 'Agent 执行配置' })
    expect(group).toHaveClass('grid-cols-3')
    expect(group).not.toHaveClass('grid-cols-1')
    expect(screen.getByText('日常创作')).toHaveClass('max-sm:hidden')
    expect(screen.getByText('需要企业版')).not.toHaveClass('max-sm:hidden')
    expect(screen.getByText('性价比').parentElement).toHaveClass('sm:flex-row', 'sm:justify-between')
    expect(screen.getByText('日常创作').parentElement).toHaveClass('sm:flex-row', 'sm:justify-between')
  })
})
