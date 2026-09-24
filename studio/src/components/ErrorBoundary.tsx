import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { isDynamicImportError } from '@/lib/lazy-with-recovery'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  handleReset = () => {
    this.setState({ hasError: false, error: null })
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }
      const assetFailure = isDynamicImportError(this.state.error)

      return (
        <div role="alert" className="flex min-h-[200px] items-center justify-center px-4">
          <Card className="w-full max-w-md">
            <CardContent className="flex flex-col items-center gap-4 pt-6 text-center">
              <div className="text-4xl">!</div>
              <div>
                <p className="text-sm font-medium text-foreground">{assetFailure ? '页面资源加载失败' : '页面出现了意外错误'}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {assetFailure ? '请检查网络后刷新页面。' : this.state.error?.message || '未知错误'}
                </p>
              </div>
              {assetFailure ? (
                <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
                  刷新页面
                </Button>
              ) : <Button variant="outline" size="sm" onClick={this.handleReset}>
                重试
              </Button>}
            </CardContent>
          </Card>
        </div>
      )
    }

    return this.props.children
  }
}
