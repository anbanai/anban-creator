import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import TimelinePage from './TimelinePage'

describe('TimelinePage filters', () => {
  it('shows meaningful labels before any filter is opened', () => {
    render(<TimelinePage />)
    for (const [name, value] of [['条目类型', '全部类型'], ['内容类型', '全部内容'], ['状态', '全部状态'], ['排序方式', '日期 ↓']]) {
      expect(screen.getByRole('combobox', { name })).toHaveTextContent(value)
    }
  })
})
