import { useSearchParams } from 'react-router-dom'
import { FlaskConical, ImageIcon, Copy } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import ViralAnalysisTab from '@/components/workshop/ViralAnalysisTab'
import PosterTab from '@/components/workshop/PosterTab'
import CloneTab from '@/components/workshop/CloneTab'

const workshopTabs = [
  { value: 'analysis', label: '爆文拆解', icon: FlaskConical },
  { value: 'poster', label: '海报制作', icon: ImageIcon },
  { value: 'clone', label: '爆款复刻', icon: Copy },
] as const

type WorkshopTab = (typeof workshopTabs)[number]['value']

export default function WorkshopPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const tabParam = searchParams.get('tab') as WorkshopTab | null
  const activeTab = tabParam && workshopTabs.some((t) => t.value === tabParam) ? tabParam : 'analysis'

  function handleTabChange(value: string) {
    setSearchParams({ tab: value })
  }

  return (
    <div className="space-y-6">
      <PageHeader title="创意工坊" description="爆文拆解、海报制作、爆款复刻，一站式内容创意工具。" />

      {/* Tab navigation */}
      <div className="flex gap-1 overflow-x-auto rounded-lg border border-border bg-muted p-1" role="tablist">
        {workshopTabs.map((tab) => (
          <button
            key={tab.value}
            role="tab"
            aria-selected={activeTab === tab.value}
            onClick={() => handleTabChange(tab.value)}
            className={`flex items-center gap-1.5 whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
              activeTab === tab.value
                ? 'bg-primary text-primary-foreground'
                : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
            }`}
          >
            <tab.icon className="h-4 w-4" />
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="min-h-[500px]">
        {activeTab === 'analysis' && <ViralAnalysisTab />}
        {activeTab === 'poster' && <PosterTab />}
        {activeTab === 'clone' && <CloneTab />}
      </div>
    </div>
  )
}
