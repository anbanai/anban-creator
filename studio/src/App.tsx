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
import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'
import AdminRoute from '@/components/auth/AdminRoute'
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts'
import { lazyWithRecovery } from '@/lib/lazy-with-recovery'

// Lazy-loaded pages
const LoginPage = lazyWithRecovery(() => import('@/pages/LoginPage'))
const RegisterPage = lazyWithRecovery(() => import('@/pages/RegisterPage'))
const DashboardPage = lazyWithRecovery(() => import('@/pages/DashboardPage'))
const TimelinePage = lazyWithRecovery(() => import('@/pages/TimelinePage'))
const ProjectsPage = lazyWithRecovery(() => import('@/pages/ProjectsPage'))
const PlansPage = lazyWithRecovery(() => import('@/pages/PlansPage'))
const TasksPage = lazyWithRecovery(() => import('@/pages/TasksPage'))
const TaskDetailPage = lazyWithRecovery(() => import('@/pages/TaskDetailPage'))
const BillingPage = lazyWithRecovery(() => import('@/pages/BillingPage'))
const UsagePage = lazyWithRecovery(() => import('@/pages/UsagePage'))
const SettingsPage = lazyWithRecovery(() => import('@/pages/SettingsPage'))
const TemplatesPage = lazyWithRecovery(() => import('@/pages/TemplatesPage'))
const SeednoteAdminPage = lazyWithRecovery(() => import('@/pages/SeednoteAdminPage'))
const PluginsPage = lazyWithRecovery(() => import('@/pages/PluginsPage'))
const SeednoteDataPage = lazyWithRecovery(() => import('@/pages/SeednoteDataPage'))
const WechatDataPage = lazyWithRecovery(() => import('@/pages/WechatDataPage'))

function LoadingSpinner() {
  return (
    <div role="status" className="flex items-center justify-center gap-3 py-16 text-sm text-muted-foreground">
      <Loader2 aria-hidden="true" className="h-5 w-5 animate-spin text-primary" />
      正在加载页面…
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
    <div className="flex min-h-dvh items-center justify-center bg-background">
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
  const { user } = useAuth()
  const isAdmin = user?.is_admin === true
  useKeyboardShortcuts(() => setShowHelp(true), { isAdmin })

  return <ShortcutHelp open={showHelp} onOpenChange={setShowHelp} isAdmin={isAdmin} />
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
        <Route path="/plugins" element={<LazyPage component={PluginsPage} />} />
        <Route path="/connect/claude-code" element={<Navigate to="/plugins?client=claude" replace />} />
        <Route path="/connect/codex" element={<Navigate to="/plugins?client=codex" replace />} />
        <Route
          element={
            <ProtectedRoute>
              <AgentPromptDropProvider>
                <NavigationProgress />
                <KeyboardShortcuts />
                <GlobalCommandPalette />
                <AppLayout />
              </AgentPromptDropProvider>
            </ProtectedRoute>
          }
        >
          <Route index element={<LazyPage component={DashboardPage} />} />
          <Route path="timeline" element={<LazyPage component={TimelinePage} />} />
          <Route path="projects" element={<LazyPage component={ProjectsPage} />} />
          <Route path="plans" element={<LazyPage component={PlansPage} />} />
          <Route path="tasks" element={<LazyPage component={TasksPage} />} />
          <Route path="tasks/:id" element={<LazyPage component={TaskDetailPage} />} />
          <Route
            path="templates"
            element={(
              <AdminRoute>
                <LazyPage component={TemplatesPage} />
              </AdminRoute>
            )}
          />
          <Route
            path="admin/seednote"
            element={(
              <AdminRoute>
                <LazyPage component={SeednoteAdminPage} />
              </AdminRoute>
            )}
          />

          <Route path="billing" element={<LazyPage component={BillingPage} />} />
          <Route path="usage" element={<LazyPage component={UsagePage} />} />
          <Route path="settings" element={<LazyPage component={SettingsPage} />} />
          <Route path="seednote-data" element={<LazyPage component={SeednoteDataPage} />} />
          <Route path="wechat-data" element={<LazyPage component={WechatDataPage} />} />
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
