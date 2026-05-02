import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import MembershipComparison from './MembershipComparison'

describe('MembershipComparison', () => {
  it('renders a cumulative membership comparison matrix', () => {
    render(<MembershipComparison currentTier="pro" />)

    expect(screen.getByRole('table', { name: '会员权益对比' })).toBeInTheDocument()
    expect(screen.getByText('免费版')).toBeInTheDocument()
    expect(screen.getByText('专业版')).toBeInTheDocument()
    expect(screen.getByText('企业版')).toBeInTheDocument()
    expect(screen.getByText('推荐')).toBeInTheDocument()
    expect(screen.getByText('当前')).toBeInTheDocument()

    expect(screen.getByText('1.0x')).toBeInTheDocument()
    expect(screen.getByText('1.2x')).toBeInTheDocument()
    expect(screen.getByText('1.5x')).toBeInTheDocument()
    expect(screen.getByText('2 个')).toBeInTheDocument()
    expect(screen.getByText('5 个')).toBeInTheDocument()
    expect(screen.getByText('10 个')).toBeInTheDocument()
    const platformRow = screen.getByRole('row', { name: /可用平台/ })
    expect(within(platformRow).getByText('Web 端')).toBeInTheDocument()
    expect(within(platformRow).getByText('Web 端、Claude Code、OpenClaw')).toBeInTheDocument()
    expect(within(platformRow).getByText('全部平台')).toBeInTheDocument()

    const customModelRow = screen.getByRole('row', { name: /自定义模型/ })
    expect(within(customModelRow).getByText('支持')).toBeInTheDocument()
    expect(within(customModelRow).getByText('包含专业版')).toBeInTheDocument()

    const advancedModelRow = screen.getByRole('row', { name: /高级模型/ })
    expect(within(advancedModelRow).getByText('企业专享')).toBeInTheDocument()

    const supportRow = screen.getByRole('row', { name: /业务指导与辅助/ })
    expect(within(supportRow).getByLabelText('包含')).toBeInTheDocument()
  })
})
