import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentPack } from '@/types'
import { AgentPackSchemaFields } from './AgentPackSchemaFields'

const schemaPack: AgentPack = {
  id: 'demo',
  version: '1.0.0',
  kind: 'managed',
  display_name: 'Demo',
  description: 'Demo Pack',
  agent: { name: 'demo', max_turns: 20 },
  bindings: { project_platforms: ['demo'], task_types: ['demo'] },
  runtime: { profile: 'article', adapter: 'standard' },
  surfaces: ['task'],
  schemas: {
    task_input: {
      type: 'object',
      required: ['tone'],
      additionalProperties: false,
      properties: {
        tone: { type: 'string', title: '语气', enum: ['concise', 'detailed'] },
        count: { type: 'integer', title: '数量', minimum: 1, maximum: 5 },
        include_sources: { type: 'boolean', title: '包含来源' },
      },
    },
  },
  ui: {},
  digest: 'a'.repeat(64),
}

describe('AgentPackSchemaFields', () => {
  it('renders supported task_input fields and emits an immutable extension object', () => {
    const onChange = vi.fn()
    render(<AgentPackSchemaFields pack={schemaPack} surface="task" value={{}} onChange={onChange} />)

    expect(screen.getByLabelText('语气')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('数量'), { target: { value: '3' } })
    expect(onChange).toHaveBeenLastCalledWith({ count: 3 })
    fireEvent.click(screen.getByRole('switch', { name: '包含来源' }))
    expect(onChange).toHaveBeenLastCalledWith({ include_sources: true })
  })

  it('does not replace registered custom scenario fields', () => {
    const customPack = { ...schemaPack, id: 'article', ui: { renderer: 'custom:article' } }
    const { container } = render(<AgentPackSchemaFields pack={customPack} surface="task" value={{}} onChange={() => {}} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('initializes declared defaults and required false booleans before submit', () => {
    const onChange = vi.fn()
    const pack: AgentPack = {
      ...schemaPack,
      schemas: {
        task_input: {
          type: 'object',
          required: ['include_sources'],
          properties: {
            count: { type: 'integer', default: 2 },
            include_sources: { type: 'boolean' },
          },
        },
      },
    }

    render(<AgentPackSchemaFields pack={pack} surface="task" value={{}} onChange={onChange} />)

    expect(onChange).toHaveBeenCalledWith({ count: 2, include_sources: false })
  })

  it('fails closed when enum options collapse to the same Select identity', () => {
    const pack: AgentPack = {
      ...schemaPack,
      schemas: {
        task_input: {
          type: 'object',
          properties: {
            code: { type: 'string', title: '代码', enum: [1, '1'] },
          },
        },
      },
    }

    render(<AgentPackSchemaFields pack={pack} surface="task" value={{}} onChange={() => {}} />)

    expect(screen.getByText('代码的枚举配置无效，暂不能提交。')).toBeInTheDocument()
    expect(screen.queryByLabelText('代码')).not.toBeInTheDocument()
  })
})
