import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import CodeBlock from '@/components/connect/CodeBlock'

interface McpConfigStepProps {
  apiKeys: Array<{ id: string; name?: string; key_prefix: string }>
}

export default function McpConfigStep({ apiKeys }: McpConfigStepProps) {
  const hasKeys = apiKeys.length > 0

  return (
    <>
      {hasKeys ? (
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
            将以下内容添加到 shell 配置文件（如 <code className="rounded bg-muted px-1 py-0.5 text-xs font-mono">~/.zshrc</code>）：
          </p>
          <CodeBlock code={`# 案板创作助手 - MCP 连接配置
export ANBANWRITER_API_KEY="你的平台API Key"       # 从下方密钥中复制完整 Key
export ANBANWRITER_API_URL="https://你的域名"       # 本地开发默认 http://localhost:18060`} />
        </>
      ) : (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-muted/20 py-6">
          <p className="text-xs text-muted-foreground text-center">你还没有创建平台密钥，需要先创建一个才能配置 MCP 连接。</p>
          <Link to="/settings">
            <Button size="sm">
              前往设置页创建密钥
              <ArrowRight className="ml-1 h-3.5 w-3.5" />
            </Button>
          </Link>
        </div>
      )}
      <p className="text-xs text-muted-foreground">
        配置完成后重启终端，或在当前终端运行 <code className="rounded bg-muted px-1 py-0.5 text-xs font-mono">source ~/.zshrc</code> 使配置生效。
      </p>
      {hasKeys && (
        <p className="text-xs text-muted-foreground">
          需要创建新密钥或管理已有密钥？
          <Link to="/settings" className="ml-1 text-primary hover:underline">前往设置页 →</Link>
        </p>
      )}
    </>
  )
}
