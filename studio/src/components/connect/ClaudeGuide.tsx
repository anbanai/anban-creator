import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ExternalLink, ArrowRight } from 'lucide-react'
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
        <p className="text-xs text-muted-foreground">
          安装后通过 <code className="rounded bg-muted px-1 py-0.5 text-xs font-mono">/plugin</code> 确认版本（当前 2.4.0）。
        </p>
      </StepCard>

      {/* Step 2: Configure MCP Connection */}
      <StepCard step={2} title="配置 MCP 连接">
        <p className="text-xs text-muted-foreground">
          插件通过 MCP 协议与平台通信，需要配置环境变量将 API Key 关联到你的账号。
        </p>
        <McpConfigStep apiKeys={apiKeys} />
      </StepCard>

      {/* Step 3: Image Generation */}
      <StepCard step={3} title="图片生成配置" optional>
        <p className="text-xs text-muted-foreground">
          图片生成需要额外配置 AI 图片服务的 Key。在 <code className="rounded bg-muted px-1 py-0.5 text-xs font-mono">~/.anbanwriter/settings.json</code> 中配置：
        </p>
        <CodeBlock
          language="json"
          code={`{
  "image": {
    "provider": "volcengine",
    "key": "你的Key",
    "base_url": "https://ark.cn-beijing.volces.com/api/v3",
    "model": "doubao-seedream-5-0-260128"
  }
}`}
        />
        <p className="text-xs text-muted-foreground mt-1">支持的图片生成服务商：</p>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border">
                <th className="py-1.5 text-left font-medium text-foreground">服务商</th>
                <th className="py-1.5 text-left font-medium text-foreground">provider 值</th>
                <th className="py-1.5 text-left font-medium text-foreground">说明</th>
              </tr>
            </thead>
            <tbody className="text-muted-foreground">
              <tr className="border-b border-border/50">
                <td className="py-1.5">OpenAI (DALL-E)</td>
                <td className="py-1.5"><code className="font-mono">openai</code></td>
                <td className="py-1.5">同步生成</td>
              </tr>
              <tr className="border-b border-border/50">
                <td className="py-1.5">Google Gemini</td>
                <td className="py-1.5"><code className="font-mono">gemini</code></td>
                <td className="py-1.5">内联图片数据</td>
              </tr>
              <tr className="border-b border-border/50">
                <td className="py-1.5">OpenRouter</td>
                <td className="py-1.5"><code className="font-mono">openrouter</code></td>
                <td className="py-1.5">多模型网关</td>
              </tr>
              <tr>
                <td className="py-1.5">火山引擎 Seedream</td>
                <td className="py-1.5"><code className="font-mono">volcengine</code></td>
                <td className="py-1.5">异步轮询</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="text-xs text-muted-foreground">
          仅做文字创作不配图可跳过此步。
        </p>
      </StepCard>

      {/* Step 4: WeChat Configuration */}
      <StepCard step={4} title="微信公众号配置" optional>
        <p className="text-xs text-muted-foreground">
          如需发布到微信公众号，需要配置 AppID 和 AppSecret。在 Claude Code 中通过自然语言或 <code className="rounded bg-muted px-1 py-0.5 text-xs font-mono">/config</code> skill 进行配置：
        </p>
        <CodeBlock code='请帮我配置微信公众号，AppID 是 xxx，Secret 是 yyy' />
        <p className="text-xs text-muted-foreground">
          只写文章不发布到微信可跳过此步。
        </p>
      </StepCard>

      {/* Quick Start */}
      <Card>
        <div className="border-b border-border px-4 py-3 flex items-center gap-2">
          <ExternalLink className="h-4 w-4 text-primary" />
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
            Agent 会自动编排研究、写作、配图、发布等全部流程，无需手动调用各个步骤。
          </p>
        </CardBody>
      </Card>
    </div>
  )
}
