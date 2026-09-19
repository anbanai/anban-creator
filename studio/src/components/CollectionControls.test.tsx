import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SearchInput } from '@/components/ui/SearchInput'
import { SimplePagination } from '@/components/SimplePagination'

describe('collection controls', () => {
  it('clears the search without losing keyboard focus', () => {
    function Search() {
      const [value, setValue] = useState('灵感')
      return <SearchInput value={value} onChange={setValue} placeholder="搜索项目" />
    }
    render(<Search />)
    fireEvent.click(screen.getByRole('button', { name: '清空搜索' }))
    expect(screen.getByRole('searchbox', { name: '搜索项目' })).toHaveValue('')
    expect(screen.getByRole('searchbox')).toHaveFocus()
  })

  it('announces the page and disables unavailable pagination actions', () => {
    const change = vi.fn()
    const { rerender } = render(<SimplePagination page={1} totalPages={3} onPageChange={change} />)
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
    expect(screen.getByText('第 1 / 3 页')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    expect(change).toHaveBeenCalledWith(2)
    rerender(<SimplePagination page={3} totalPages={3} onPageChange={change} />)
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
  })
})
