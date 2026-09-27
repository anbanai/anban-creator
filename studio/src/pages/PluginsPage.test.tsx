import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import PluginsPage from './PluginsPage'

describe('PluginsPage', () => {
  it('switches between Claude Code, Codex, and DeepSeek Harness one-line installation prompts', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })

    render(<MemoryRouter initialEntries={['/plugins']}><PluginsPage /></MemoryRouter>)

    expect(screen.getByText(/creator\.anbanai\.com\/claude/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Codex' }))
    expect(screen.getByText(/creator\.anbanai\.com\/codex/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '复制安装指令' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining('/codex')))
  })

  it('deep-links the DeepSeek Harness prompt from the client query', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })

    render(<MemoryRouter initialEntries={['/plugins?client=dsh']}><PluginsPage /></MemoryRouter>)

    expect(screen.getByText(/creator\.anbanai\.com\/dsh/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '复制安装指令' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining('/dsh')))
  })

  it('falls back to Claude Code for an unknown client query', () => {
    render(<MemoryRouter initialEntries={['/plugins?client=unknown']}><PluginsPage /></MemoryRouter>)
    expect(screen.getByText(/creator\.anbanai\.com\/claude/)).toBeInTheDocument()
  })

  it('links users to the API key settings anchor', () => {
    render(<MemoryRouter><PluginsPage /></MemoryRouter>)
    expect(screen.getByRole('link', { name: /获取 API Key/ })).toHaveAttribute('href', '/settings#api-key-settings')
  })
})
