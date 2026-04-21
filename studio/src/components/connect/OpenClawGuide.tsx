import { Construction } from 'lucide-react'

export default function OpenClawGuide() {
  return (
    <div className="flex flex-col items-center justify-center py-20 space-y-3">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Construction className="h-6 w-6 text-muted-foreground" />
      </div>
      <h3 className="text-sm font-medium text-foreground">即将支持</h3>
      <p className="text-xs text-muted-foreground text-center max-w-xs">
        OpenClaw 接入指南正在准备中，敬请期待。
      </p>
    </div>
  )
}
