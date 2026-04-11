import { BrowserRouter, Routes, Route, Navigate, useLocation, Link } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import React, { useState, Suspense } from 'react'
import { Loader2 } from 'lucide-react'
import { AuthProvider, useAuth } from '@/contexts/AuthContext'
import { Toaster } from '@/components/ui/sonner'
import AppLayout from '@/components/layout/AppLayout'
import ShortcutHelp from '@/components/ShortcutHelp'
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts'

// Lazy-loaded pages
const LoginPage = React.lazy(() => import('@/pages/LoginPage'))
const RegisterPage = React.lazy(() => import('@/pages/RegisterPage'))
const DashboardPage = React.lazy(() => import('@/pages/DashboardPage'))
const TimelinePage = React.lazy(() => import('@/pages/TimelinePage'))
const ChannelsPage = React.lazy(() => import('@/pages/ChannelsPage'))
const PlansPage = React.lazy(() => import('@/pages/PlansPage'))
const TasksPage = React.lazy(() => import('@/pages/TasksPage'))
const TaskDetailPage = React.lazy(() => import('@/pages/TaskDetailPage'))
const CreditsPage = React.lazy(() => import('@/pages/CreditsPage'))
const SettingsPage = React.lazy(() => import('@/pages/SettingsPage'))

function LoadingSpinner() {
  return (
    <div className="flex items-center justify-center py-16">
      <Loader2 className="h-8 w-8 animate-spin text-primary" />
    </div>
  )
}

function NotFoundPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="text-center">
        <h1 className="text-6xl font-bold text-foreground">404</h1>
        <p className="mt-4 text-muted-foreground">页面未找到</p>
        <Link
          to="/"
          className="mt-6 inline-block rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          返回首页
        </Link>
      </div>
    </div>
  )
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()
  const location = useLocation()

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  return <>{children}</>
}

function PublicRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()

  if (isAuthenticated) {
    return <Navigate to="/" replace />
  }

  return <>{children}</>
}

function KeyboardShortcuts() {
  const [showHelp, setShowHelp] = useState(false)
  useKeyboardShortcuts(() => setShowHelp(true))

  return <ShortcutHelp open={showHelp} onOpenChange={setShowHelp} />
}

function AppRoutes() {
  return (
    <Routes>
      <Route
        path="/login"
        element={
          <PublicRoute>
            <Suspense fallback={<LoadingSpinner />}>
              <LoginPage />
            </Suspense>
          </PublicRoute>
        }
      />
      <Route
        path="/register"
        element={
          <PublicRoute>
            <Suspense fallback={<LoadingSpinner />}>
              <RegisterPage />
            </Suspense>
          </PublicRoute>
        }
      />
      <Route
        element={
          <ProtectedRoute>
            <KeyboardShortcuts />
            <AppLayout />
          </ProtectedRoute>
        }
      >
        <Route index element={<Suspense fallback={<LoadingSpinner />}><DashboardPage /></Suspense>} />
        <Route path="timeline" element={<Suspense fallback={<LoadingSpinner />}><TimelinePage /></Suspense>} />
        <Route path="channels" element={<Suspense fallback={<LoadingSpinner />}><ChannelsPage /></Suspense>} />
        <Route path="plans" element={<Suspense fallback={<LoadingSpinner />}><PlansPage /></Suspense>} />
        <Route path="tasks" element={<Suspense fallback={<LoadingSpinner />}><TasksPage /></Suspense>} />
        <Route path="tasks/:id" element={<Suspense fallback={<LoadingSpinner />}><TaskDetailPage /></Suspense>} />
        <Route path="credits" element={<Suspense fallback={<LoadingSpinner />}><CreditsPage /></Suspense>} />
        <Route path="settings" element={<Suspense fallback={<LoadingSpinner />}><SettingsPage /></Suspense>} />
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
          <AuthProvider>
            <Toaster richColors position="top-right" />
            <AppRoutes />
          </AuthProvider>
        </ThemeProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
