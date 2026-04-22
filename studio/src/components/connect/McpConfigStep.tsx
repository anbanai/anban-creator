import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { Button } from '@/components/ui/Button'

interface McpConfigStepProps {
  apiKeys: Array<{ id: string; name?: string; key_prefix: string }>
}

export default function McpConfigStep({ apiKeys }: McpConfigStepProps) {
  const hasKeys = apiKeys.length > 0

  if (!hasKeys) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-muted/20 py-6">
        <p className="text-xs text-muted-foreground text-center">你还没有创建平台密钥，需要先创建一个才能完成连接。</p>
        <Link to="/settings">
          <Button size="sm">
            前往设置页创建密钥
            <ArrowRight className="ml-1 h-3.5 w-3.5" />
          </Button>
        </Link>
      </div>
    )
  }

  return (
    <>
      <div className="rounded-lg border border-border bg-muted/30 px-3 py-2 space-y-1">
        <p className="text-xs font-medium text-foreground">你的平台密钥</p>
        {apiKeys.map((key) => (
          <p key={key.id} className="text-xs text-muted-foreground font-mono">
            {key.name || '未命名'}: {key.key_prefix}{'*'.repeat(20)}
          </p>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        将上方的完整密钥复制到插件配置中，即可完成平台连接。
      </p>
      <p className="text-xs text-muted-foreground">
        需要创建新密钥或管理已有密钥？
        <Link to="/settings" className="ml-1 text-primary hover:underline">前往设置页 →</Link>
      </p>
    </>
  )
}
