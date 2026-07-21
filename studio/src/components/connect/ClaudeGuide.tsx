import { useQuery } from '@tanstack/react-query'
import { Card, CardContent } from '@/components/ui/card'
import CodeBlock from '@/components/connect/CodeBlock'
import StepCard from '@/components/connect/StepCard'
import InstallStep from '@/components/connect/InstallStep'
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
        <CardContent className="space-y-2">
          <h2 className="text-sm font-semibold text-foreground">Anban 智能创作助手 Claude Code 插件</h2>
          <p className="text-xs leading-relaxed text-muted-foreground">
            通过 Claude Code 插件，你可以用自然语言直接驱动 AI 创作流程。推荐按「注册账号 → 创建 Key → 安装插件 → 配置 Key → /setup → 重启 → 开始使用」这条路径完成接入，第一次配置会最顺。
          </p>
        </CardContent>
      </Card>

      <StepCard step={1} title="注册或登录 Anban 账号">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            先打开 Anban Studio / Web 管理端，完成注册或登录。没有平台账号的话，后面的插件无法连上服务。
          </p>
          <CodeBlock code="https://creator.anbanai.com" language="text" />
        </div>
      </StepCard>

      <StepCard step={2} title="创建 API Key">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            登录后进入设置页创建 API Key。建议按设备命名，比如 `My MacBook`。注意：完整 Key 只会在创建成功时显示一次。
          </p>
          <CodeBlock code="https://creator.anbanai.com/settings" language="text" />
          <McpConfigStep apiKeys={apiKeys} />
        </div>
      </StepCard>

      <InstallStep
        cliName="Claude Code CLI"
        oneLiner="帮我安装 Anban Creator Claude Code 插件 https://github.com/royalmorty/anbanwriter/tree/main/plugins/anban"
        oneLinerHint="AI 会自动添加 marketplace 源并完成插件安装。"
        advancedCli={`git clone https://github.com/royalmorty/anbanwriter.git
cd anbanwriter
claude plugin marketplace add ./plugins/anban
claude plugin install --scope user anban@anbanai`}
        advancedHint="`anban@anbanai` 表示插件 ID `anban` 来自发布方 `anbanai`；MCP server key 固定为 `creator`，具体工具名由 Claude Code 运行时处理。"
      />

      <StepCard step={4} title="配置 API Key">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            把完整 API Key 写入 Claude Code 的用户级配置。推荐写入 `~/.claude/settings.json`，这样所有项目都能复用。
          </p>
          <CodeBlock
            code={`{
  "env": {
    "ANBAN_API_KEY": "你的完整 API Key"
  }
}`}
            language="json"
          />
          <p className="text-xs text-muted-foreground">
            如果这个文件原来已经有别的配置，只需要把 `env` 字段合并进去，不要覆盖已有内容。
          </p>
          <p className="text-xs text-muted-foreground">
            如果你使用官方在线服务，可以额外配置 `ANBAN_API_URL=https://api.creator.anbanai.com`；如果你接的是自建或本地服务，就填你自己的服务地址。
          </p>
        </div>
      </StepCard>

      <StepCard step={5} title="运行 /setup">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            安装并配置好 Key 后，运行初始化命令，检查 API Key、MCP 服务和账号连接是否正常：
          </p>
          <CodeBlock code="/setup" />
        </div>
      </StepCard>

      <StepCard step={6} title="重启并再次验证">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            `/setup` 完成后，请完全退出并重新启动 Claude Code。重启后再运行一次 `/setup`，确认连接真的已经生效。
          </p>
          <CodeBlock code={`/setup
/plugin`} />
        </div>
      </StepCard>

      <StepCard step={7} title="开始使用">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">你可以直接说需求，也可以指定 Agent 启动固定流程：</p>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">自然语言示例：</p>
            <CodeBlock
              code={`帮我写一篇关于 AI Agent 的公众号文章
种草笔记，主题是降噪耳机`}
              language="text"
            />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">指定 Agent 示例：</p>
            <CodeBlock
              code={`claude --dangerously-skip-permissions --verbose --agent anban:article AI Agent 入门指南
claude --dangerously-skip-permissions --verbose --agent anban:seednote 降噪耳机种草笔记`}
            />
          </div>
          <p className="text-xs text-muted-foreground">
            如果只是第一次验证，优先跑 `anban:article` 或直接输入一条自然语言需求，最容易确认整条链路是否通了。
          </p>
        </div>
      </StepCard>
    </div>
  )
}
