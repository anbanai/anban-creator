import { useState, useRef, useEffect } from 'react'
import { useTheme } from 'next-themes'
import { Sun, Moon, Monitor } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'

const themeOptions = [
  { value: 'light', label: '亮色模式', icon: Sun },
  { value: 'dark', label: '暗色模式', icon: Moon },
  { value: 'system', label: '跟随系统', icon: Monitor },
] as const

export default function UserAccountPopover({ collapsed }: { collapsed?: boolean }) {
  const { user, logout } = useAuth()
  const { theme, setTheme } = useTheme()
  const [mounted, setMounted] = useState(false)
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)

  useEffect(() => setMounted(true), [])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  if (!user) return null

  const initials = user.nickname
    ? user.nickname.slice(0, 1).toUpperCase()
    : user.email.slice(0, 1).toUpperCase()

  const currentTheme = mounted && theme ? theme : 'system'

  return (
    <div className="relative" ref={popoverRef}>
      <button
        onClick={() => setOpen(!open)}
        className={`flex items-center rounded-lg text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground ${
          collapsed ? "justify-center p-2" : "gap-2 px-3 py-1.5"
        }`}
      >
        {user.avatar ? (
          <img
            src={user.avatar}
            alt={user.nickname}
            className="h-7 w-7 rounded-full object-cover"
          />
        ) : (
          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-blue-600 text-xs font-medium text-white">
            {initials}
          </span>
        )}
        {!collapsed && <span className="hidden sm:inline max-w-[120px] truncate">{user.nickname || user.email}</span>}
      </button>

      {open && (
        <div className={`absolute bottom-full mb-1 w-48 rounded-lg border border-border bg-popover py-1 shadow-lg ${
          collapsed ? "left-0" : "right-0"
        }`}>
          <div className="border-b border-border px-4 py-2">
            <p className="truncate text-sm font-medium text-popover-foreground">{user.nickname || '用户'}</p>
            <p className="truncate text-xs text-muted-foreground">{user.email}</p>
          </div>
          <div className="border-b border-border py-1">
            {themeOptions.map((option) => (
              <button
                key={option.value}
                onClick={() => setTheme(option.value)}
                className={`flex w-full items-center gap-2 px-4 py-1.5 text-left text-sm transition-colors hover:bg-accent hover:text-foreground ${
                  currentTheme === option.value ? 'text-popover-foreground' : 'text-muted-foreground'
                }`}
              >
                <option.icon className="h-4 w-4" />
                {option.label}
                {currentTheme === option.value && <span className="ml-auto text-xs">✓</span>}
              </button>
            ))}
          </div>
          <button
            onClick={() => {
              setOpen(false)
              void logout()
            }}
            className="w-full px-4 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            退出登录
          </button>
        </div>
      )}
    </div>
  )
}
