import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ExecutionProfileSelector } from './ExecutionProfileSelector'

const profiles = [
  { id: 'cost_effective' as const, display_name: '性价比', model_name: 'DeepSeek 4 Pro', model_id: 'deepseek-v4-pro', description: '日常创作', min_tier: 'free' as const, available: true },
  { id: 'balanced' as const, display_name: '平衡型', model_name: '豆包 Seed Evolving', model_id: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro' as const, available: true },
  { id: 'maximum_quality' as const, display_name: '极致效果', model_name: 'Kimi K3（1M）', model_id: 'k3', description: '复杂高质量创作', min_tier: 'enterprise' as const, available: false, unavailable_reason: 'requires_enterprise' },
]

describe('ExecutionProfileSelector', () => {
  it('shows every server profile, model name, availability, and disabled reason', () => {
    render(<ExecutionProfileSelector profiles={profiles} value="cost_effective" onChange={() => {}} />)

    expect(screen.getByText('性价比')).toBeInTheDocument()
    expect(screen.getByText('平衡型')).toBeInTheDocument()
    expect(screen.getByText('极致效果')).toBeInTheDocument()
    expect(screen.getByText('DeepSeek 4 Pro')).toBeInTheDocument()
    expect(screen.getByText('豆包 Seed Evolving')).toBeInTheDocument()
    expect(screen.getByText('Kimi K3（1M）')).toBeInTheDocument()
    expect(screen.getByText('deepseek-v4-pro')).toBeInTheDocument()
    expect(screen.getByText('doubao-seed-evolving')).toBeInTheDocument()
    expect(screen.getByText('k3')).toBeInTheDocument()
    expect(screen.getByText('全部用户')).toBeInTheDocument()
    expect(screen.getByText('Pro 版及以上')).toBeInTheDocument()
    expect(screen.getByText('企业版')).toBeInTheDocument()
    expect(screen.getByText('需要企业版')).toBeInTheDocument()
    expect(screen.queryByText('requires_enterprise')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /极致效果/ })).toBeDisabled()
  })

  it('reports an available profile selection without clearing the current value', () => {
    const onChange = vi.fn()
    function Harness() {
      const [value, setValue] = useState<'cost_effective' | 'balanced' | 'maximum_quality'>('cost_effective')
      return <ExecutionProfileSelector profiles={profiles} value={value} onChange={(next) => { setValue(next); onChange(next) }} />
    }
    render(<Harness />)

    fireEvent.click(screen.getByRole('button', { name: /平衡型/ }))
    expect(onChange).toHaveBeenCalledWith('balanced')

    fireEvent.click(screen.getByRole('button', { name: /性价比/ }))
    expect(onChange).toHaveBeenLastCalledWith('cost_effective')
  })
})
