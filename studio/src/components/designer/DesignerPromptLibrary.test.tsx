import { fireEvent, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import DesignerPromptLibrary from './DesignerPromptLibrary'

it('filters image previews by tag and fills the selected template', async () => {
  const onSelect = vi.fn()
  render(<DesignerPromptLibrary open={false} onOpenChange={vi.fn()} onSelect={onSelect} />)
  const library = within(screen.getByRole('complementary', { name: '提示词库' }))
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(7)
  fireEvent.click(library.getByRole('combobox'))
  fireEvent.change(await screen.findByPlaceholderText('搜索标签...'), { target: { value: 'SKU' } })
  expect(screen.queryByRole('option', { name: '电商主图' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('option', { name: '电商 SKU 图' }))
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(2)
  expect(library.queryByRole('button', { name: '使用极简留白主图提示词' })).not.toBeInTheDocument()
  fireEvent.click(library.getByRole('button', { name: '使用多规格对比提示词' }))
  expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({
    title: '多规格对比', prompt: expect.stringContaining('【规格清单】'),
  }))
  fireEvent.click(library.getByRole('combobox'))
  fireEvent.click(await screen.findByRole('option', { name: '电商白底图' }))
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(1)
  fireEvent.click(library.getByRole('button', { name: '使用纯白商品图提示词' }))
  expect(onSelect).toHaveBeenLastCalledWith(expect.objectContaining({
    title: '纯白商品图', prompt: expect.stringContaining('#FFFFFF'),
  }))
})

it('combines search with the selected tag and can reset an empty result', async () => {
  render(<DesignerPromptLibrary open={false} onOpenChange={vi.fn()} onSelect={vi.fn()} />)
  const library = within(screen.getByRole('complementary', { name: '提示词库' }))
  fireEvent.change(library.getByRole('textbox', { name: '搜索提示词' }), { target: { value: '  留白主图  ' } })
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(1)
  expect(library.getByRole('button', { name: '使用极简留白主图提示词' })).toBeInTheDocument()
  fireEvent.click(library.getByRole('combobox'))
  fireEvent.click(await screen.findByRole('option', { name: '电商 SKU 图' }))
  expect(library.queryByRole('button', { name: /^使用.+提示词$/ })).not.toBeInTheDocument()
  expect(library.getByText('没有找到匹配的提示词')).toBeInTheDocument()
  fireEvent.click(library.getByRole('button', { name: '重置筛选' }))
  expect(library.getByRole('textbox', { name: '搜索提示词' })).toHaveValue('')
  expect(library.getAllByRole('button', { name: /^使用.+提示词$/ })).toHaveLength(7)
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
