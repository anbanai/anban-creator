import { useQuery } from '@tanstack/react-query'
import { Card, CardContent } from '@/components/ui/card'
import CodeBlock from '@/components/connect/CodeBlock'
import StepCard from '@/components/connect/StepCard'
import InstallStep from '@/components/connect/InstallStep'
import McpConfigStep from '@/components/connect/McpConfigStep'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export default function CodexGuide() {
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
          <h2 className="text-sm font-semibold text-foreground">Anban 智能创作助手 Codex 插件</h2>
          <p className="text-xs leading-relaxed text-muted-foreground">
            通过 Codex 原生插件，你可以在 OpenAI Codex CLI 中用自然语言驱动 AI 创作流程，并调用专门的 subagent 完成端到端流水线。推荐按「注册账号 → 创建 Key → 安装插件 → 配置 Key → $setup → 重启 → 开始使用」这条顺序接入。
          </p>
        </CardContent>
      </Card>

      <StepCard step={1} title="注册或登录 Anban 账号">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            先打开 Anban Studio / Web 管理端，完成注册或登录。没有平台账号的话，Codex 插件无法连接平台服务。
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

      <InstallStep
        cliName="Codex CLI"
        oneLiner="帮我安装 Anban Creator Codex 插件 https://github.com/anbanai/anban-creator-codex"
        oneLinerHint="AI 会自动完成 marketplace 注册、插件安装、以及 5 个 subagent 的注册。"
        advancedCli={`git clone https://github.com/anbanai/anban-creator-codex.git
cd anban-creator-codex
codex plugin marketplace add .
codex plugin install anban
bash install/install-subagents.sh`}
        advancedHint="最后一行脚本会把 5 个 subagent 注册到 `~/.codex/config.toml`（幂等，可重复执行）。"
      />

      <StepCard step={4} title="配置 API Key">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            Codex 插件通过环境变量读取平台连接信息。最少只需要配置 API Key：
          </p>
          <CodeBlock
            code={`export ANBAN_API_KEY="你的完整 API Key"`}
          />
          <p className="text-xs text-muted-foreground">
            把上面这行写入 `~/.zshrc`（或 `~/.bashrc`），然后执行 `source ~/.zshrc`，或者直接关闭并重新打开一个新的终端会话。如果你使用官方在线服务，可以再加一行 `export ANBAN_API_URL="https://api.creator.anbanai.com"`；接自建或本地服务就填你自己的服务地址。
          </p>
        </div>
      </StepCard>

      <StepCard step={5} title="运行 $setup">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            安装并配置好 Key 后，运行初始化命令，让插件检查 API Key、MCP 服务和账号连接是否正常：
          </p>
          <CodeBlock code="$setup" />
        </div>
      </StepCard>

      <StepCard step={6} title="重启并再次验证">
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            `$setup` 跑完以后，请完全退出并重新启动 Codex。重启后再执行一次 `$setup`，确认连接已经正式生效。
          </p>
          <CodeBlock code="$setup" />
        </div>
      </StepCard>

      <StepCard step={7} title="开始使用">
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">你可以直接说需求，也可以指定 subagent 启动端到端流水线：</p>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">自然语言示例：</p>
            <CodeBlock
              code={`写一篇关于 AI Agent 的公众号文章
种草笔记，主题是降噪耳机`}
              language="text"
            />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">指定 subagent 示例：</p>
            <CodeBlock
              code={`use the wechatarticle subagent to write a 3000-word article about Rust ownership
use the seednote subagent for a 种草笔记 about 降噪耳机
delegate to designer: colorize the line art at /path/to/lineart/ using a warm summer palette`}
              language="text"
            />
          </div>
          <p className="text-xs text-muted-foreground">
            第一次验证时，优先跑 `use the wechatarticle subagent` 或直接说一条自然语言需求，最容易确认整条链路是否通了。
          </p>
        </div>
      </StepCard>
    </div>
  )
}
