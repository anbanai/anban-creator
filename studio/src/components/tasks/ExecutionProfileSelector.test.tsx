import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ExecutionProfileSelector } from './ExecutionProfileSelector'

const profiles = [
  { id: 'cost_effective' as const, display_name: '性价比', provider: 'deepseek', protocol: 'anthropic' as const, models: { default: 'deepseek-v4-flash', opus: 'deepseek-v4-pro', fable: 'deepseek-v4-flash', sonnet: 'deepseek-v4-pro', haiku: 'deepseek-v4-flash' }, claude: {}, description: '日常创作', min_tier: 'free' as const, available: true },
  { id: 'balanced' as const, display_name: '平衡型', provider: 'volcengine_ark', protocol: 'anthropic' as const, models: { default: 'doubao-seed-evolving', opus: 'doubao-seed-evolving', fable: 'doubao-seed-evolving', sonnet: 'doubao-seed-evolving', haiku: 'doubao-seed-evolving' }, claude: {}, description: '质量与速度平衡', min_tier: 'pro' as const, available: true },
  { id: 'maximum_quality' as const, display_name: '极致效果', provider: 'moonshot', protocol: 'anthropic' as const, models: { default: 'kimi-k3[1m]', opus: 'kimi-k3[1m]', fable: 'kimi-k3[1m]', sonnet: 'kimi-k3[1m]', haiku: 'kimi-k3[1m]' }, claude: { effort_level: 'high' as const }, description: '复杂高质量创作', min_tier: 'enterprise' as const, available: false, unavailable_reason: 'requires_enterprise' },
]

describe('ExecutionProfileSelector', () => {
  it('shows every server profile, provider, availability, and disabled reason', () => {
    render(<ExecutionProfileSelector profiles={profiles} value="cost_effective" onChange={() => {}} />)

    expect(screen.getByText('性价比')).toBeInTheDocument()
    expect(screen.getByText('平衡型')).toBeInTheDocument()
    expect(screen.getByText('极致效果')).toBeInTheDocument()
    expect(screen.getByText('deepseek')).toBeInTheDocument()
    expect(screen.getByText('volcengine_ark')).toBeInTheDocument()
    expect(screen.getByText('moonshot')).toBeInTheDocument()
    expect(screen.getByText('全部角色：doubao-seed-evolving')).toBeInTheDocument()
    expect(screen.getByText('全部角色：kimi-k3[1m]')).toBeInTheDocument()
    expect(screen.getByText('全部用户')).toBeInTheDocument()
    expect(screen.getByText('Pro 版及以上')).toBeInTheDocument()
    expect(screen.getByText('企业版')).toBeInTheDocument()
    expect(screen.getByText('需要企业版')).toBeInTheDocument()
    expect(screen.queryByText('requires_enterprise')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /极致效果/ })).toBeDisabled()
  })

  it('shows split model roles on demand', () => {
    render(<ExecutionProfileSelector profiles={profiles} value="cost_effective" onChange={() => {}} />)

    expect(screen.getByText('默认：deepseek-v4-flash')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /查看性价比模型配置/ }))
    expect(screen.getByText('Opus：deepseek-v4-pro')).toBeInTheDocument()
    expect(screen.getByText('Sonnet：deepseek-v4-pro')).toBeInTheDocument()
  })

  it('reports an available profile selection without clearing the current value', () => {
    const onChange = vi.fn()
    function Harness() {
      const [value, setValue] = useState<'cost_effective' | 'balanced' | 'maximum_quality'>('cost_effective')
      return <ExecutionProfileSelector profiles={profiles} value={value} onChange={(next) => { setValue(next); onChange(next) }} />
    }
    render(<Harness />)

    fireEvent.click(screen.getByRole('button', { name: /^平衡型，/ }))
    expect(onChange).toHaveBeenCalledWith('balanced')

    fireEvent.click(screen.getByRole('button', { name: /^性价比，/ }))
    expect(onChange).toHaveBeenLastCalledWith('cost_effective')
  })
})
