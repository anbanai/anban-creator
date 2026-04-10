import { useEffect, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'

interface ShortcutMap {
  [key: string]: () => void
}

export function useKeyboardShortcuts(onShowHelp?: () => void) {
  const navigate = useNavigate()
  const sequenceRef = useRef('')
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const shortcuts: ShortcutMap = {
    'g d': () => navigate('/'),
    'g c': () => navigate('/channels'),
    'g p': () => navigate('/plans'),
    'g t': () => navigate('/tasks'),
    'g l': () => navigate('/timeline'),
    'g $': () => navigate('/credits'),
    'g s': () => navigate('/settings'),
    '?': () => onShowHelp?.(),
  }

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    // Ignore if user is typing in an input, textarea, or contenteditable
    const target = e.target as HTMLElement
    if (
      target.tagName === 'INPUT' ||
      target.tagName === 'TEXTAREA' ||
      target.tagName === 'SELECT' ||
      target.isContentEditable
    ) {
      return
    }

    // Ignore if modifier keys are pressed (except for ?)
    if (e.metaKey || e.ctrlKey || e.altKey) return

    const key = e.key.toLowerCase()

    // Single key shortcuts
    if (key === '?') {
      e.preventDefault()
      shortcuts['?']()
      return
    }

    // Sequence shortcuts (g + key)
    if (sequenceRef.current === '' && key === 'g') {
      sequenceRef.current = 'g'
      // Reset sequence after 1 second
      timerRef.current = setTimeout(() => {
        sequenceRef.current = ''
      }, 1000)
      return
    }

    if (sequenceRef.current === 'g') {
      const sequence = `g ${key}`
      if (shortcuts[sequence]) {
        e.preventDefault()
        clearTimeout(timerRef.current)
        sequenceRef.current = ''
        shortcuts[sequence]()
      } else {
        sequenceRef.current = ''
      }
    }
  }, [navigate, onShowHelp])

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [handleKeyDown])
}

export const SHORTCUT_LIST = [
  { keys: 'g d', description: '前往仪表盘' },
  { keys: 'g c', description: '前往渠道' },
  { keys: 'g p', description: '前往计划' },
  { keys: 'g t', description: '前往任务' },
  { keys: 'g l', description: '前往时间线' },
  { keys: 'g $', description: '前往积分' },
  { keys: 'g s', description: '前往设置' },
  { keys: '?', description: '显示快捷键帮助' },
]
