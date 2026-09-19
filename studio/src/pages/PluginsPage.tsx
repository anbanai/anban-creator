import { useEffect, useMemo, useState } from 'react'
import { Check, ChevronRight, Copy, KeyRound, PlugZap, TerminalSquare } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'

type PluginClient = 'claude' | 'codex'

const clientContent: Record<PluginClient, {
  label: string
  guideUrl: string
  prompt: string
  detail: string
}> = {
  claude: {
    label: 'Claude Code',
    guideUrl: 'https://creator.anbanai.com/claude',
    prompt: '阅读 https://creator.anbanai.com/claude，帮我安装并配置 Anban Creator 插件。需要 ANBAN_API_KEY 时向我索取。',
    detail: '粘贴到 Claude Code 的任意会话。Agent 会读取专用说明并完成安装。',
  },
  codex: {
    label: 'Codex',
    guideUrl: 'https://creator.anbanai.com/codex',
    prompt: '阅读 https://creator.anbanai.com/codex，帮我安装并配置 Anban Creator 插件。需要 ANBAN_API_KEY 时向我索取。',
    detail: '粘贴到 Codex 的任意任务。Agent 会读取专用说明并完成安装。',
  },
}

function clientFromQuery(value: string | null): PluginClient {
  return value === 'codex' ? 'codex' : 'claude'
}

export default function PluginsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const client = clientFromQuery(searchParams.get('client'))
  const content = clientContent[client]
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    document.title = 'Anban Creator Agent Plugin'
  }, [])

  const guidePath = useMemo(() => new URL(content.guideUrl).pathname, [content.guideUrl])

  async function copyPrompt() {
    await navigator.clipboard.writeText(content.prompt)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1800)
  }

  function selectClient(next: PluginClient) {
    setCopied(false)
    setSearchParams({ client: next }, { replace: true })
  }

  return (
    <main className="min-h-dvh bg-[#f7f7f5] text-[#171716] dark:bg-[#111210] dark:text-[#f4f4f0]">
      <header className="border-b border-black/8 bg-white/85 dark:border-white/10 dark:bg-[#111210]/90">
        <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-5 sm:px-8">
          <Link to="/" className="flex items-center gap-2.5 text-sm font-semibold tracking-normal">
            <span className="flex h-8 w-8 items-center justify-center rounded-md bg-[#c2413b] text-white">
              <PlugZap className="h-4 w-4" aria-hidden="true" />
            </span>
            Anban Creator
          </Link>
          <Link to="/" className="inline-flex items-center gap-1 text-sm text-black/60 hover:text-black dark:text-white/60 dark:hover:text-white">
            进入 Studio
            <ChevronRight className="h-4 w-4" aria-hidden="true" />
          </Link>
        </div>
      </header>

      <div className="mx-auto grid max-w-5xl gap-12 px-5 py-14 sm:px-8 sm:py-20 lg:grid-cols-[minmax(0,1.45fr)_minmax(260px,0.75fr)] lg:gap-16">
        <section>
          <div className="mb-8">
            <p className="mb-4 flex items-center gap-2 text-sm font-medium text-[#c2413b] dark:text-[#ef8a84]">
              <TerminalSquare className="h-4 w-4" aria-hidden="true" />
              Agent Plugin
            </p>
            <h1 className="max-w-2xl text-4xl font-semibold leading-[1.08] tracking-normal sm:text-5xl">
              Anban Creator
            </h1>
            <p className="mt-5 max-w-xl text-base leading-7 text-muted-foreground">
              把公众号、种草笔记、Montage 视频生产与发布流程接入你正在使用的 Agent。
            </p>
          </div>

          <div className="mb-5 inline-flex h-10 rounded-md border border-black/10 bg-white p-1 dark:border-white/10 dark:bg-white/5" aria-label="选择 Agent">
            {(Object.keys(clientContent) as PluginClient[]).map((item) => (
              <button
                key={item}
                type="button"
                onClick={() => selectClient(item)}
                className={`min-w-32 rounded px-4 text-sm font-medium transition-colors ${
                  item === client
                    ? 'bg-[#171716] text-white dark:bg-white dark:text-[#171716]'
                    : 'text-black/55 hover:text-black dark:text-white/55 dark:hover:text-white'
                }`}
              >
                {clientContent[item].label}
              </button>
            ))}
          </div>

          <div className="overflow-hidden rounded-md border border-black/10 bg-[#181916] text-white shadow-[0_18px_50px_rgba(0,0,0,0.12)] dark:border-white/12">
            <div className="flex h-11 items-center justify-between border-b border-white/10 px-4">
              <span className="text-xs font-medium text-white/55">一句话安装到 {content.label}</span>
              <button
                type="button"
                onClick={() => void copyPrompt()}
                className="flex h-8 w-8 items-center justify-center rounded text-white/65 hover:bg-white/10 hover:text-white"
                aria-label="复制安装指令"
                title="复制安装指令"
              >
                {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
              </button>
            </div>
            <div className="px-5 py-6 sm:px-6 sm:py-7">
              <p className="font-mono text-sm leading-7 text-white/92 sm:text-[15px]">{content.prompt}</p>
            </div>
          </div>
          <p className="mt-4 text-sm leading-6 text-muted-foreground">{content.detail}</p>
          <a href={guidePath} className="mt-5 inline-flex items-center gap-1.5 text-sm font-medium text-[#a93631] hover:text-[#7f2925] dark:text-[#ef8a84]">
            查看给 {content.label} 的完整说明
            <ChevronRight className="h-4 w-4" aria-hidden="true" />
          </a>
        </section>

        <aside className="border-t border-black/10 pt-8 dark:border-white/10 lg:border-l lg:border-t-0 lg:pl-10 lg:pt-2">
          <div className="mb-8 flex h-10 w-10 items-center justify-center rounded-md bg-[#dbe8df] text-[#245b39] dark:bg-[#21402d] dark:text-[#a8ddb8]">
            <KeyRound className="h-5 w-5" aria-hidden="true" />
          </div>
          <h2 className="text-lg font-semibold">获取 ANBAN_API_KEY</h2>
          <ol className="mt-6 space-y-5 text-sm leading-6 text-muted-foreground">
            <li><span className="mr-2 font-mono text-xs text-muted-foreground">01</span>注册或登录 Anban Creator。</li>
            <li><span className="mr-2 font-mono text-xs text-muted-foreground">02</span>在设置页创建一个平台密钥。</li>
            <li><span className="mr-2 font-mono text-xs text-muted-foreground">03</span>立即保存完整密钥，它只展示一次。</li>
            <li><span className="mr-2 font-mono text-xs text-muted-foreground">04</span>仅在 Agent 安装过程询问时提供。</li>
          </ol>
          <Link
            to="/settings#api-key-settings"
            className="mt-8 inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-[#c2413b] px-4 text-sm font-medium text-white hover:bg-[#a93631]"
          >
            获取 API Key
            <ChevronRight className="h-4 w-4" aria-hidden="true" />
          </Link>
          <p className="mt-3 text-xs leading-5 text-muted-foreground">不要把密钥写进聊天记录、截图、代码仓库或公开日志。</p>
        </aside>
      </div>
    </main>
  )
}
