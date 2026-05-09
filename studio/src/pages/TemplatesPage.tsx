import PageHeader from '@/components/layout/PageHeader'
import { TemplateGrid } from '@/components/templates/TemplateGrid'

export default function TemplatesPage() {
  return (
    <div className="space-y-6">
      <PageHeader title="模板库" description="浏览和使用内容模板，快速创建高质量内容。">
      </PageHeader>

      <TemplateGrid />
    </div>
  )
}
