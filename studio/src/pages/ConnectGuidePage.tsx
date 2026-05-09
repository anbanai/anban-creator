import PageHeader from '@/components/layout/PageHeader'
import ClaudeGuide from '@/components/connect/ClaudeGuide'

export default function ConnectGuidePage() {
  return (
    <div className="space-y-6">
      <PageHeader
        title="Claude Code 接入指南"
        description="配置 Claude Code 插件连接，开始自动化创作"
        breadcrumbs={[{ label: '设置', href: '/settings' }, { label: 'Claude Code 接入指南' }]}
      />
      <ClaudeGuide />
    </div>
  )
}
