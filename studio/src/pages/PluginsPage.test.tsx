import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import PluginsPage from './PluginsPage'

describe('PluginsPage', () => {
  it('switches between Claude Code and Codex one-line installation prompts', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })

    render(<MemoryRouter initialEntries={['/plugins']}><PluginsPage /></MemoryRouter>)

    expect(screen.getByText(/creator\.anbanai\.com\/claude/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Codex' }))
    expect(screen.getByText(/creator\.anbanai\.com\/codex/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '复制安装指令' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining('/codex')))
  })

  it('links users to the API key settings anchor', () => {
    render(<MemoryRouter><PluginsPage /></MemoryRouter>)
    expect(screen.getByRole('link', { name: /获取 API Key/ })).toHaveAttribute('href', '/settings#api-key-settings')
  })
})
