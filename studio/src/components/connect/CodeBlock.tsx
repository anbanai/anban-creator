import { useState } from 'react'
import { Check, Copy } from 'lucide-react'

interface CodeBlockProps {
  code: string
  language?: string
}

export default function CodeBlock({ code, language = 'bash' }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(code).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="group relative rounded-lg border border-border bg-muted/50">
      <button
        onClick={handleCopy}
        className="absolute right-2 top-2 rounded-md p-1.5 text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground group-hover:opacity-100"
        title="复制代码"
        aria-label={copied ? '已复制' : '复制代码'}
      >
        {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
      <pre className="overflow-x-auto p-3 text-xs leading-relaxed">
        <code className={`language-${language} font-mono text-foreground`}>{code}</code>
      </pre>
    </div>
  )
}
