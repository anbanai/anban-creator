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
      <Card>
        <CardBody className="space-y-2">
          <h2 className="text-sm font-semibold text-foreground">案板创作助手 Claude Code 插件</h2>
          <p className="text-xs text-muted-foreground leading-relaxed">
            通过 Claude Code 插件，你可以用自然语言直接驱动 AI 创作流程。插件支持微信公众号图文、小红书笔记、小绿书图片帖、鲜花图片等多种内容类型的端到端自动化创作——从选题研究、AI 风格化写作、智能配图到草稿发布，全部自动编排。
          </p>
        </CardBody>
      </Card>

      <StepCard step={1} title="安装插件">
        <p className="text-xs text-muted-foreground">
          在 Claude Code 中安装案板创作助手插件：
        </p>
        <CodeBlock code="/install-plugin anbanai/anbanwriter" />
      </StepCard>

      <StepCard step={2} title="连接平台账号">
        <p className="text-xs text-muted-foreground">
          安装后需要关联你的平台密钥，才能正常使用插件功能：
        </p>
        <McpConfigStep apiKeys={apiKeys} />
      </StepCard>

      <StepCard step={3} title="开始使用">
        <p className="text-xs text-muted-foreground">
          配置完成后，直接用自然语言描述你想创作的内容即可，例如：
        </p>
        <div className="space-y-1.5 text-xs font-mono text-muted-foreground">
          <p>帮我写一篇关于 AI Agent 的文章</p>
          <p>小红书种草笔记，主题是降噪耳机</p>
          <p>小绿书图片帖，主题是春日穿搭</p>
        </div>
      </StepCard>
    </div>
  )
}
