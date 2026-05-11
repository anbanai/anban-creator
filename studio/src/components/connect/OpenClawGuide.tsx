import { useQuery } from '@tanstack/react-query'
import { Card, CardContent } from '@/components/ui/Card'
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
        <CardContent className="space-y-2">
          <h2 className="text-sm font-semibold text-foreground">Anban 智能创作助手 OpenClaw 插件</h2>
          <p className="text-xs leading-relaxed text-muted-foreground">
            通过 OpenClaw 原生插件，你可以在 OpenClaw 中使用斜杠命令或自然语言驱动 AI 创作流程。推荐按「注册账号 → 创建 Key → 安装插件 → 配置 Key → /init → 重启 → 开始使用」这条顺序接入。
          </p>
        </CardContent>
      </Card>

      <StepCard step={1} title="注册或登录 Anban 账号">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            先打开 Anban Studio / Web 管理端，完成注册或登录。没有平台账号的话，OpenClaw 插件无法连接平台服务。
          </p>
          <CodeBlock code="https://creator.anbanai.com" language="text" />
        </div>
      </StepCard>

      <StepCard step={2} title="创建 API Key">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            登录后进入设置页创建 API Key。建议按设备命名，方便后面管理。注意：完整 Key 只在创建成功时展示一次。
          </p>
          <CodeBlock code="https://creator.anbanai.com/settings" language="text" />
          <McpConfigStep apiKeys={apiKeys} />
        </div>
      </StepCard>

      <StepCard step={3} title="安装插件">
        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">先克隆插件仓库：</p>
            <CodeBlock code={`git clone https://github.com/anbanai/anbanwriter-openclaw.git\ncd anbanwriter-openclaw`} />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">然后安装为 OpenClaw 原生插件：</p>
            <CodeBlock code="openclaw plugins install ./" />
          </div>
        </div>
      </StepCard>

      <StepCard step={4} title="配置 API Key">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            OpenClaw 插件通过环境变量读取平台连接信息。最少只需要配置 API Key：
          </p>
          <CodeBlock
            code={`export ANBANWRITER_API_KEY="你的完整 API Key"`}
          />
          <p className="text-xs text-muted-foreground">
            如果你使用官方在线服务，可以再加一行 `export ANBANWRITER_API_URL="https://api.creator.anbanai.com"`；如果你接的是自建或本地服务，就填你自己的服务地址。写入后执行 `source ~/.zshrc`，或者直接关闭并重新打开一个新的终端会话。
          </p>
        </div>
      </StepCard>

      <StepCard step={5} title="运行 /init">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            安装并配置好 Key 后，运行 `/init`，让插件检查 API Key、MCP 服务和账号连接：
          </p>
          <CodeBlock code="/init" />
        </div>
      </StepCard>

      <StepCard step={6} title="重启并再次验证">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            `/init` 跑完以后，请完全退出并重新启动 OpenClaw。重启后再执行一次 `/init`，确认连接已经正式生效。
          </p>
          <CodeBlock code="/init" />
        </div>
      </StepCard>

      <StepCard step={7} title="开始使用">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">你可以直接执行斜杠命令，也可以自然语言描述需求：</p>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">斜杠命令示例：</p>
            <CodeBlock
              code={`/article AI Agent 入门指南
/rednote 降噪耳机种草笔记
/xls 春日穿搭图片帖
/flower 春日鲜花摄影`}
            />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">自然语言示例：</p>
            <CodeBlock
              code={`帮我写一篇关于 AI Agent 的公众号文章
小红书种草笔记，主题是降噪耳机
小绿书图片帖，主题是春日穿搭
帮我生成一组郁金香的鲜花图片`}
              language="text"
            />
          </div>
        </div>
      </StepCard>
    </div>
  )
}
