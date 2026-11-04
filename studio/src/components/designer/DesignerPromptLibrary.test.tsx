import { fireEvent, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import DesignerPromptLibrary from './DesignerPromptLibrary'

it('filters image previews by tag and fills the selected template', () => {
  const onSelect = vi.fn()
  render(<DesignerPromptLibrary open={false} onOpenChange={vi.fn()} onSelect={onSelect} />)
  const library = within(screen.getByRole('complementary', { name: '提示词库' }))
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(8)
  fireEvent.click(library.getByRole('button', { name: '电商 SKU 图' }))
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(2)
  expect(library.queryByRole('button', { name: '使用极简留白主图提示词' })).not.toBeInTheDocument()
  fireEvent.click(library.getByRole('button', { name: '使用多规格对比提示词' }))
  expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({
    title: '多规格对比', prompt: expect.stringContaining('【规格清单】'),
  }))
})

it('prevents filling a template while generation is in progress', () => {
  const onSelect = vi.fn()
  render(<DesignerPromptLibrary open={false} onOpenChange={vi.fn()} onSelect={onSelect} disabled />)
  const library = within(screen.getByRole('complementary', { name: '提示词库' }))
  const template = library.getByRole('button', { name: '使用极简留白主图提示词' })
  expect(template).toBeDisabled()
  fireEvent.click(template)
  expect(onSelect).not.toHaveBeenCalled()
})
