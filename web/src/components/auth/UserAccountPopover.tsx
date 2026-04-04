import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@/contexts/AuthContext'

export default function UserAccountPopover() {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)

  // Close on outside click
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

  return (
    <div className="relative" ref={popoverRef}>
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm text-gray-300 transition-colors hover:bg-gray-800 hover:text-gray-100"
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
        <span className="hidden sm:inline max-w-[120px] truncate">{user.nickname || user.email}</span>
      </button>

      {open && (
        <div className="absolute right-0 top-full mt-1 w-48 rounded-lg border border-gray-700 bg-gray-800 py-1 shadow-lg">
          <div className="border-b border-gray-700 px-4 py-2">
            <p className="truncate text-sm font-medium text-gray-100">{user.nickname || '用户'}</p>
            <p className="truncate text-xs text-gray-400">{user.email}</p>
          </div>
          <button
            onClick={() => {
              setOpen(false)
              void logout()
            }}
            className="w-full px-4 py-2 text-left text-sm text-gray-300 transition-colors hover:bg-gray-700 hover:text-gray-100"
          >
            退出登录
          </button>
        </div>
      )}
    </div>
  )
}
