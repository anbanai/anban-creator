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
      <Card>
        <CardBody className="space-y-2">
          <h2 className="text-sm font-semibold text-foreground">Anban 智能创作助手 OpenClaw 插件</h2>
          <p className="text-xs text-muted-foreground leading-relaxed">
            通过 OpenClaw 原生插件，你可以在 OpenClaw 中使用斜杠命令或自然语言驱动 AI 创作流程。插件内置 18 个专业技能，支持微信公众号图文、小红书笔记、小绿书图片帖、鲜花图片等多种内容类型的端到端自动化创作。
          </p>
        </CardBody>
      </Card>

      <StepCard step={1} title="安装插件">
        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              1. 克隆插件仓库：
            </p>
            <CodeBlock code={`git clone https://github.com/anbanai/anbanwriter-openclaw.git\ncd anbanwriter-openclaw`} />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              2. 安装为 OpenClaw 原生插件：
            </p>
            <CodeBlock code="openclaw plugins install ./" />
          </div>
        </div>
      </StepCard>

      <StepCard step={2} title="连接平台账号">
        <p className="text-xs text-muted-foreground">
          安装后需要关联你的平台密钥，才能正常使用插件功能：
        </p>
        <McpConfigStep apiKeys={apiKeys} />
      </StepCard>

      <StepCard step={3} title="开始使用">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            用斜杠命令快速启动创作，或直接用自然语言描述：
          </p>
          <div className="space-y-1.5 text-xs font-mono text-muted-foreground">
            <p>/article AI Agent 入门指南</p>
            <p>/rednote 降噪耳机种草笔记</p>
            <p>/xls 春日穿搭图片帖</p>
            <p>/flower 春日鲜花摄影</p>
          </div>
        </div>
      </StepCard>
    </div>
  )
}
