import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import ModelCapabilities from './ModelCapabilities'

describe('ModelCapabilities', () => {
  it('renders three tier cards with model categories', () => {
    render(<ModelCapabilities currentTier="pro" />)

    expect(screen.getByText('模型能力')).toBeInTheDocument()
    expect(screen.getByText('免费版')).toBeInTheDocument()
    expect(screen.getByText('专业版')).toBeInTheDocument()
    expect(screen.getByText('企业版')).toBeInTheDocument()
    expect(screen.getByText('推荐')).toBeInTheDocument()
    expect(screen.getByText('当前')).toBeInTheDocument()
  })

  it('renders exactly 6 available checks (basic: 3 + advanced: 1 + custom: 2)', () => {
    render(<ModelCapabilities currentTier="free" />)
    expect(screen.getAllByLabelText('包含').length).toBe(6)
  })

  it('renders exactly 3 unavailable marks (basic: 0 + advanced: 2 + custom: 1)', () => {
    render(<ModelCapabilities currentTier="free" />)
    expect(screen.getAllByLabelText('不包含').length).toBe(3)
  })

  it('shows legend with all three category descriptions', () => {
    render(<ModelCapabilities currentTier="enterprise" />)
    expect(screen.getByText('日常任务的高性价比选择')).toBeInTheDocument()
    expect(screen.getByText(/顶级模型/)).toBeInTheDocument()
    expect(screen.getByText(/绑定自己的 API Key/)).toBeInTheDocument()
  })
})
