import { Outlet } from 'react-router-dom'
import { PageTransition } from '@/components/PageTransition'
import Sidebar from './Sidebar'
import FeedbackFab from './FeedbackFab'

export default function AppLayout() {
  return (
    <div className="flex h-dvh bg-background text-foreground">
      <Sidebar />
      <main className="flex-1 overflow-auto px-4 py-6 md:px-8 md:py-8">
        <PageTransition>
          <Outlet />
        </PageTransition>
      </main>
      <FeedbackFab />
    </div>
  )
}
