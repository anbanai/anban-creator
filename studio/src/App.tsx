import { BrowserRouter, Routes, Route, Navigate, useLocation, Link } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import React, { useState, Suspense } from 'react'
import { Loader2 } from 'lucide-react'
import { AuthProvider, useAuth } from '@/contexts/AuthContext'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import AppLayout from '@/components/layout/AppLayout'
import { NavigationProgress } from '@/components/NavigationProgress'
import ShortcutHelp from '@/components/ShortcutHelp'
import GlobalCommandPalette from '@/components/GlobalCommandPalette'
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
const UsagePage = React.lazy(() => import('@/pages/UsagePage'))
const SettingsPage = React.lazy(() => import('@/pages/SettingsPage'))
const TemplatesPage = React.lazy(() => import('@/pages/TemplatesPage'))

const DesignerPage = React.lazy(() => import('@/pages/DesignerPage'))
const ClaudeCodeGuidePage = React.lazy(() => import('@/pages/ConnectGuidePage'))
const OpenClawGuidePage = React.lazy(() => import('@/pages/OpenClawGuidePage'))
const CodexGuidePage = React.lazy(() => import('@/pages/CodexGuidePage'))

function LoadingSpinner() {
  return (
    <div className="flex items-center justify-center py-16">
      <Loader2 className="h-8 w-8 animate-spin text-primary" />
    </div>
  )
}

function LazyPage({ component: Component }: { component: React.LazyExoticComponent<React.ComponentType> }) {
  return (
    <Suspense fallback={<LoadingSpinner />}>
      <ErrorBoundary>
        <Component />
      </ErrorBoundary>
    </Suspense>
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
    <ErrorBoundary>
      <Routes>
        <Route
          path="/login"
          element={
            <PublicRoute>
              <LazyPage component={LoginPage} />
            </PublicRoute>
          }
        />
        <Route
          path="/register"
          element={
            <PublicRoute>
              <LazyPage component={RegisterPage} />
            </PublicRoute>
          }
        />
        <Route
          element={
            <ProtectedRoute>
              <NavigationProgress />
              <KeyboardShortcuts />
              <GlobalCommandPalette />
              <AppLayout />
            </ProtectedRoute>
          }
        >
          <Route index element={<LazyPage component={DashboardPage} />} />
          <Route path="timeline" element={<LazyPage component={TimelinePage} />} />
          <Route path="channels" element={<LazyPage component={ChannelsPage} />} />
          <Route path="plans" element={<LazyPage component={PlansPage} />} />
          <Route path="tasks" element={<LazyPage component={TasksPage} />} />
          <Route path="tasks/:id" element={<LazyPage component={TaskDetailPage} />} />
          <Route path="templates" element={<LazyPage component={TemplatesPage} />} />

          <Route path="designer" element={<LazyPage component={DesignerPage} />} />
          <Route path="credits" element={<LazyPage component={CreditsPage} />} />
          <Route path="usage" element={<LazyPage component={UsagePage} />} />
          <Route path="settings" element={<LazyPage component={SettingsPage} />} />
          <Route path="connect/claude-code" element={<LazyPage component={ClaudeCodeGuidePage} />} />
          <Route path="connect/openclaw" element={<LazyPage component={OpenClawGuidePage} />} />
          <Route path="connect/codex" element={<LazyPage component={CodexGuidePage} />} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </ErrorBoundary>
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
            <TooltipProvider>
              <Toaster richColors position="bottom-right" closeButton duration={4000} />
              <AppRoutes />
            </TooltipProvider>
          </AuthProvider>
        </ThemeProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
