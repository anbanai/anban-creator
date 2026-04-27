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
          <h2 className="text-sm font-semibold text-foreground">Anban 智能创作助手 Claude Code 插件</h2>
          <p className="text-xs text-muted-foreground leading-relaxed">
            通过 Claude Code 插件，你可以用自然语言直接驱动 AI 创作流程。插件支持微信公众号图文、小红书笔记、小绿书图片帖、鲜花图片等多种内容类型的端到端自动化创作——从选题研究、AI 风格化写作、智能配图到草稿发布，全部自动编排。
          </p>
        </CardBody>
      </Card>

      <StepCard step={1} title="安装插件">
        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              1. 添加插件市场源：
            </p>
            <CodeBlock code="claude plugin marketplace add anbanai/anbanwriter-claudecode" />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              2. 安装插件：
            </p>
            <CodeBlock code="claude plugin install --scope user anbanwriter@anbanai" />
          </div>
        </div>
      </StepCard>

      <StepCard step={2} title="连接平台账号">
        <p className="text-xs text-muted-foreground">
          安装后需要关联你的平台密钥，才能正常使用插件功能：
        </p>
        <McpConfigStep apiKeys={apiKeys} />
      </StepCard>

      <StepCard step={3} title="初始化配置">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            安装完成后，在终端运行初始化命令，完成 API 密钥配置和连接验证：
          </p>
          <CodeBlock code="/init" />
        </div>
      </StepCard>

      <StepCard step={4} title="开始使用">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            用自然语言或指定 Agent 直接启动创作：
          </p>
          <div className="space-y-1.5 text-xs font-mono text-muted-foreground">
            <p>claude --dangerously-skip-permissions --verbose --agent anbanwriter:rednote 降噪耳机种草笔记</p>
            <p>claude --dangerously-skip-permissions --verbose --agent anbanwriter:article AI Agent 入门指南</p>
            <p>claude --dangerously-skip-permissions --verbose --agent anbanwriter:xls 春日穿搭图片帖</p>
            <p>claude --dangerously-skip-permissions --verbose --agent anbanwriter:flower 春日鲜花摄影</p>
          </div>
        </div>
      </StepCard>
    </div>
  )
}
