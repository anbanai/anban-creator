import { useState } from 'react'
import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { ProfileDimensionCard } from './ProfileDimensionCard'

describe('ProfileDimensionCard', () => {
  it('shows readable values and evidence without exposing a JSON editor by default', () => {
    render(<ProfileDimensionCard label="定位" dimension={{ content: { name: '老张有好茶', audience: ['刚开始喝茶的人'], active: false, unknown: null }, sources: ['[推断待确认]'], evidence: ['来自用户提供的简介'], missing_fields: ['产品范围'] }} text="{}" onTextChange={vi.fn()} />)
    expect(screen.getByText('老张有好茶')).toBeInTheDocument()
    expect(screen.getByText('刚开始喝茶的人')).toBeInTheDocument()
    expect(screen.queryByText('是', { exact: true })).not.toBeInTheDocument()
    expect(screen.getByText('否')).toBeInTheDocument()
    expect(screen.getByText('尚不确定')).toBeInTheDocument()
    expect(screen.getByText(/推断待确认/)).toBeInTheDocument()
    expect(screen.getByText('来自用户提供的简介')).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('edits nested text without dropping unknown fields, array elements or data types', () => {
    const change = vi.fn()
    const content = { details: { name: '旧名称', count: 3, enabled: false, unknown: null }, readers: ['新手', '资深爱好者'], extra: { preserved: true } }
    render(<ProfileDimensionCard label="定位" dimension={{ content, sources: [], evidence: [], missing_fields: [] }} text={JSON.stringify(content)} onTextChange={change} />)
    fireEvent.click(screen.getByRole('button', { name: '修改定位' }))
    fireEvent.change(screen.getByLabelText('定位 · details · 名称'), { target: { value: '新名称' } })
    expect(JSON.parse(change.mock.calls[0][0])).toEqual({ ...content, details: { ...content.details, name: '新名称' } })
    fireEvent.change(screen.getByLabelText('定位 · readers 2'), { target: { value: '老茶客' } })
    expect(JSON.parse(change.mock.calls[1][0])).toEqual({ ...content, readers: ['新手', '老茶客'] })
  })

  it('keeps focus while adding a plain-language summary to an empty dimension', () => {
    function Example() {
      const [content, setContent] = useState<Record<string, unknown>>({})
      return <ProfileDimensionCard label="定位" dimension={{ content, sources: [], evidence: [], missing_fields: [] }} text={JSON.stringify(content)} onTextChange={(text) => setContent(JSON.parse(text))} />
    }
    render(<Example />)
    fireEvent.click(screen.getByRole('button', { name: '修改定位' }))
    const input = screen.getByLabelText('定位 · 概述')
    input.focus()
    fireEvent.change(input, { target: { value: '白' } })
    expect(screen.getByLabelText('定位 · 概述')).toBe(input)
    expect(input).toHaveFocus()
    fireEvent.change(input, { target: { value: '白茶商家' } })
    expect(input).toHaveValue('白茶商家')
  })
})
