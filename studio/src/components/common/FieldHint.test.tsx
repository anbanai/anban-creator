import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'

import { FieldHint } from './FieldHint'

function renderHint(children: React.ReactNode, label?: string) {
  return render(
    <TooltipProvider>
      <FieldHint label={label}>{children}</FieldHint>
    </TooltipProvider>,
  )
}

describe('FieldHint', () => {
  it('exposes the explanation through the trigger accessible name and tooltip', () => {
    renderHint('写入 AGENTS.md，Agent 创作时会自动遵循此定位。')

    const trigger = screen.getByRole('button', { name: '写入 AGENTS.md，Agent 创作时会自动遵循此定位。' })
    fireEvent.mouseEnter(trigger)
    fireEvent.focus(trigger)

    expect(trigger).toHaveAttribute('type', 'button')
    expect(trigger).toHaveClass('cursor-help')
  })

  it('keeps a custom accessible name without leaking the icon into the name', () => {
    renderHint(<span>说明</span>, '关于项目定位')

    const trigger = screen.getByRole('button', { name: '关于项目定位' })
    expect(trigger).toBeInTheDocument()
  })
})
