import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import StudioEntryPage from './StudioEntryPage'
vi.mock('./AssistantWorkspace', () => ({ default: () => <div>实际对话入口</div> }))
vi.mock('./MobileStudioPage', () => ({ default: () => <div>手机创作工作台</div> }))
vi.mock('@/hooks/use-mobile', () => ({ useIsMobile: () => true }))

it('opens the mobile home after login and enters the real interview only on an explicit action', () => {
  function StartInterview() { const navigate = useNavigate(); return <button onClick={() => navigate('/', { state: { modeId: 'profile', projectId: '', startNew: true } })}>建立人设</button> }
  render(<MemoryRouter><StudioEntryPage /><StartInterview /></MemoryRouter>)
  expect(screen.getByText('手机创作工作台')).toBeInTheDocument()
  expect(screen.queryByText('实际对话入口')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '建立人设' }))
  expect(screen.getByText('实际对话入口')).toBeInTheDocument()
})
