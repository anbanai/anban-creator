import { useQuery } from '@tanstack/react-query'
import { Card, CardBody } from '@/components/ui/Card'
import CodeBlock from '@/components/connect/CodeBlock'
import StepCard from '@/components/connect/StepCard'
import McpConfigStep from '@/components/connect/McpConfigStep'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export default function ClaudeGuide() {
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
          <h2 className="text-sm font-semibold text-foreground">案板创作助手 Claude Code 插件</h2>
          <p className="text-xs text-muted-foreground leading-relaxed">
            通过 Claude Code 插件，你可以用自然语言直接驱动 AI 创作流程。插件支持微信公众号图文、小红书笔记、小绿书图片帖、鲜花图片等多种内容类型的端到端自动化创作——从选题研究、AI 风格化写作、智能配图到草稿发布，全部自动编排。
          </p>
        </CardBody>
      </Card>

      {/* Step 1: Install Plugin */}
      <StepCard step={1} title="安装插件">
        <p className="text-xs text-muted-foreground">
          在 Claude Code 中安装案板创作助手插件：
        </p>
        <CodeBlock code="/install-plugin anbanai/anbanwriter" />
      </StepCard>

      {/* Step 2: Configure Connection */}
      <StepCard step={2} title="连接平台账号">
        <p className="text-xs text-muted-foreground">
          安装后需要关联你的平台密钥，才能正常使用插件功能：
        </p>
        <McpConfigStep apiKeys={apiKeys} />
      </StepCard>

      {/* Step 3: WeChat Configuration */}
      <StepCard step={3} title="微信公众号配置" optional>
        <p className="text-xs text-muted-foreground">
          如需发布到微信公众号，需要配置公众号的 AppID 和 Secret。首次发布时，插件会引导你完成配置。
        </p>
        <p className="text-xs text-muted-foreground">
          只写文章不发布到微信可跳过此步。
        </p>
      </StepCard>

      {/* Quick Start */}
      <Card>
        <div className="border-b border-border px-4 py-3 flex items-center gap-2">
          <svg className="h-4 w-4 text-primary" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
          </svg>
          <h3 className="text-sm font-semibold text-foreground">快速开始</h3>
        </div>
        <CardBody className="space-y-2">
          <p className="text-xs text-muted-foreground">
            配置完成后，用自然语言即可触发自动化创作：
          </p>
          <div className="space-y-1.5 text-xs font-mono text-muted-foreground">
            <p>帮我写一篇关于 AI Agent 的文章</p>
            <p>小红书种草笔记，主题是降噪耳机</p>
            <p>小绿书图片帖，主题是春日穿搭</p>
            <p>帮我生成一组郁金香的鲜花图片</p>
          </div>
          <p className="text-xs text-muted-foreground">
            AI 会自动编排研究、写作、配图、发布等全部流程，无需手动操作。
          </p>
        </CardBody>
      </Card>
    </div>
  )
}
