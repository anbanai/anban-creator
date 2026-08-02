import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'

import { useAuth } from '@/contexts/AuthContext'

export default function AdminRoute({ children }: { children: ReactNode }) {
  const { user } = useAuth()

  if (user && typeof user.is_admin !== 'boolean') return null
  if (!user?.is_admin) return <Navigate to="/" replace />
  return <>{children}</>
}
