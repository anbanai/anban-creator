import PageHeader from '@/components/layout/PageHeader'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import ClaudeGuide from '@/components/connect/ClaudeGuide'
import OpenClawGuide from '@/components/connect/OpenClawGuide'

export default function ConnectGuidePage() {
  return (
    <div className="space-y-6">
      <PageHeader title="AI 接入指南" description="选择你的 AI 平台，配置连接后即可开始自动化创作" />

      <Tabs defaultValue="claude">
        <TabsList className="w-full sm:w-auto">
          <TabsTrigger value="claude" className="flex-1 sm:flex-none px-6">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5z" />
              <path d="M2 17l10 5 10-5" />
              <path d="M2 12l10 5 10-5" />
            </svg>
            Claude Code
          </TabsTrigger>
          <TabsTrigger value="openclaw" className="flex-1 sm:flex-none px-6">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M15 6v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3z" />
            </svg>
            OpenClaw
          </TabsTrigger>
        </TabsList>

        <TabsContent value="claude" className="mt-6">
          <ClaudeGuide />
        </TabsContent>
        <TabsContent value="openclaw" className="mt-6">
          <OpenClawGuide />
        </TabsContent>
      </Tabs>
    </div>
  )
}
