import PageHeader from '@/components/layout/PageHeader'
import { TemplateGrid } from '@/components/templates/TemplateGrid'

export default function TemplatesPage() {
  return (
    <div className="space-y-6">
      <PageHeader title="模板库" description="管理小红书视觉与版式模板。">
      </PageHeader>

      <TemplateGrid />
    </div>
  )
}
