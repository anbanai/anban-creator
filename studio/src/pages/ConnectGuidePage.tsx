import { useState } from 'react'
import { Bot, Wrench } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import ProviderNav from '@/components/connect/ProviderNav'
import type { Provider } from '@/components/connect/ProviderNav'
import ClaudeGuide from '@/components/connect/ClaudeGuide'
import OpenClawGuide from '@/components/connect/OpenClawGuide'

const providers: Provider[] = [
  { id: 'claude', name: 'Claude', icon: Bot, status: 'available' },
  { id: 'openclaw', name: 'OpenClaw', icon: Wrench, status: 'coming-soon' },
]

export default function ConnectGuidePage() {
  const [activeProvider, setActiveProvider] = useState('claude')

  return (
    <div className="space-y-6">
      <PageHeader title="AI 接入指南" description="配置 AI 助手连接，开始自动化创作" />

      <div className="flex flex-col md:flex-row gap-6">
        <ProviderNav
          providers={providers}
          activeId={activeProvider}
          onSelect={setActiveProvider}
        />
        <div className="flex-1 min-w-0">
          {activeProvider === 'claude' && <ClaudeGuide />}
          {activeProvider === 'openclaw' && <OpenClawGuide />}
        </div>
      </div>
    </div>
  )
}
