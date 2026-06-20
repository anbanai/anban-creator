import PageHeader from '@/components/layout/PageHeader'
import CodexGuide from '@/components/connect/CodexGuide'

export default function CodexGuidePage() {
  return (
    <div className="space-y-6">
      <PageHeader
        title="Codex 接入指南"
        description="配置 Codex 插件连接，开始自动化创作"
        breadcrumbs={[{ label: '设置', href: '/settings' }, { label: 'Codex 接入指南' }]}
      />
      <CodexGuide />
    </div>
  )
}
