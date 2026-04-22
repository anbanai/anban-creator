import PageHeader from '@/components/layout/PageHeader'
import OpenClawGuide from '@/components/connect/OpenClawGuide'

export default function OpenClawGuidePage() {
  return (
    <div className="space-y-6">
      <PageHeader title="OpenClaw 接入指南" description="配置 OpenClaw 插件连接，开始自动化创作" />
      <OpenClawGuide />
    </div>
  )
}
