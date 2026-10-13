import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

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
      <Tooltip>
        <TooltipTrigger render={<button type="button" onClick={handleCopy} className="absolute right-2 top-2 flex size-11 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100" aria-label={copied ? '已复制' : '复制代码'} />}>
          {copied ? <Check className="size-4 text-green-500" /> : <Copy className="size-4" />}
        </TooltipTrigger>
        <TooltipContent>{copied ? '已复制' : '复制代码'}</TooltipContent>
      </Tooltip>
      <pre className="overflow-x-auto p-3 text-xs leading-relaxed">
        <code className={`language-${language} font-mono text-foreground`}>{code}</code>
      </pre>
    </div>
  )
}
