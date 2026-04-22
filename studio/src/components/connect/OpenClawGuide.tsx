import { useQuery } from '@tanstack/react-query'
import { Card, CardBody } from '@/components/ui/Card'
import CodeBlock from '@/components/connect/CodeBlock'
import StepCard from '@/components/connect/StepCard'
import McpConfigStep from '@/components/connect/McpConfigStep'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export default function OpenClawGuide() {
  const { data: apiKeys = [] } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: async () => {
      const data = await api.apiKeys.list()
      return data.items || []
    },
  })

  return (
    <div className="space-y-4">
      {/* Overview */}
      <Card>
        <CardBody className="space-y-2">
          <h2 className="text-sm font-semibold text-foreground">案板创作助手 OpenClaw 插件</h2>
          <p className="text-xs text-muted-foreground leading-relaxed">
            通过 OpenClaw 原生插件，你可以在 OpenClaw 平台中使用自然语言驱动 AI 创作流程。插件支持微信公众号图文、小红书笔记、小绿书图片帖、鲜花图片等多种内容类型的自动化创作。
          </p>
          <div className="flex flex-wrap gap-1.5 pt-1">
            <span className="rounded bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">自动化工作流</span>
            <span className="rounded bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">斜杠命令</span>
            <span className="rounded bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">AI 配图</span>
            <span className="rounded bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">质量检查</span>
          </div>
        </CardBody>
      </Card>

      {/* Step 1: Install Plugin */}
      <StepCard step={1} title="安装插件">
        <p className="text-xs text-muted-foreground">
          将案板创作助手安装为 OpenClaw 原生插件：
        </p>
        <CodeBlock code="openclaw plugins install ./openclaw" />
      </StepCard>

      {/* Step 2: Configure Connection */}
      <StepCard step={2} title="连接平台账号">
        <p className="text-xs text-muted-foreground">
          安装后需要关联你的平台密钥，才能正常使用插件功能：
        </p>
        <McpConfigStep apiKeys={apiKeys} />
      </StepCard>

      {/* Step 3: Image Generation */}
      <StepCard step={3} title="图片生成配置" optional>
        <p className="text-xs text-muted-foreground">
          如需使用 AI 配图功能，需要额外配置图片服务的密钥。首次使用配图功能时，插件会引导你完成配置。
        </p>
        <p className="text-xs text-muted-foreground">仅做文字创作不配图可跳过此步。</p>
      </StepCard>

      {/* Usage */}
      <Card>
        <div className="border-b border-border px-4 py-3 flex items-center gap-2">
          <svg className="h-4 w-4 text-primary" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
          </svg>
          <h3 className="text-sm font-semibold text-foreground">使用方式</h3>
        </div>
        <CardBody className="space-y-3">
          <div>
            <p className="text-xs font-medium text-foreground mb-1.5">自然语言触发</p>
            <p className="text-xs text-muted-foreground mb-2">
              输入包含关键词的消息，AI 会自动识别并开始创作：
            </p>
            <div className="space-y-1 text-xs font-mono text-muted-foreground">
              <p>帮我写一篇关于 AI Agent 的文章</p>
              <p>小红书种草笔记，主题是降噪耳机</p>
              <p>小绿书图片帖，主题是春日穿搭</p>
              <p>帮我生成一组郁金香的鲜花图片</p>
            </div>
          </div>
          <div className="border-t border-border pt-3">
            <p className="text-xs font-medium text-foreground mb-1.5">斜杠命令</p>
            <p className="text-xs text-muted-foreground mb-2">
              也可以通过斜杠命令直接触发：
            </p>
            <div className="space-y-1 text-xs font-mono text-muted-foreground">
              <p>/wechat-article AI Agent 技术趋势</p>
              <p>/wechat-xls 春日旅行</p>
              <p>/rednote 降噪耳机推荐</p>
              <p>/flower 郁金香</p>
            </div>
          </div>
        </CardBody>
      </Card>
    </div>
  )
}
