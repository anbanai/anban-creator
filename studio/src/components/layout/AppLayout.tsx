import { Outlet, useLocation } from 'react-router-dom'
import { PageTransition } from '@/components/PageTransition'
import Sidebar from './Sidebar'
import FeedbackFab from './FeedbackFab'

export default function AppLayout() {
  const location = useLocation()
  const isAssistantWorkspace = location.pathname === '/'
  return (
    <div className="flex h-dvh bg-background text-foreground">
      <a href="#main-content" className="sr-only z-[100] rounded-lg bg-primary px-4 py-3 text-sm text-primary-foreground focus:not-sr-only focus:fixed focus:left-4 focus:top-4">跳转到主要内容</a>
      {!isAssistantWorkspace && <Sidebar />}
      <main id="main-content" tabIndex={-1} className={`flex min-h-0 min-w-0 flex-1 flex-col overflow-auto outline-none ${isAssistantWorkspace ? '' : 'px-4 pb-8 pt-20 md:px-8 md:py-8 lg:px-10'}`}>
        <PageTransition>
          <Outlet />
        </PageTransition>
        {!isAssistantWorkspace && <FeedbackFab />}
      </main>
    </div>
  )
}
