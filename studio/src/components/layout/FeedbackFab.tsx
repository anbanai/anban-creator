import { useState } from 'react'
import { MessageCircle } from 'lucide-react'
import { toast } from 'sonner'

import { useSubmitLock } from '@/hooks/useSubmitLock'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/common/button'
import { Textarea } from '@/components/ui/textarea'
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'
import { feedbackApi } from '@/lib/api/feedback'

type FeedbackType = 'bug' | 'suggestion'

export default function FeedbackFab() {
  const [open, setOpen] = useState(false)

  // Feedback form state
  const [type, setType] = useState<FeedbackType>('bug')
  const [content, setContent] = useState('')
  const { submit, isSubmitting } = useSubmitLock()

  async function handleSubmit() {
    const trimmed = content.trim()
    if (!trimmed) return
    try {
      await submit(async () => {
        await feedbackApi.create({ type, content: trimmed })
        toast.success('反馈提交成功，感谢您的建议！')
        setContent('')
        setType('bug')
        setOpen(false)
      })
    } catch {
      toast.error('反馈提交失败，内容已保留，请稍后重试。')
    }
  }

  return (
    <div
      role="region"
      className="relative z-40 mt-4 flex justify-end md:fixed md:bottom-20 md:right-6 md:mt-0"
      aria-label="反馈"
    >
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverContent side="top" align="end" sideOffset={10} className="w-80 max-w-[calc(100vw-2rem)] p-0">
          <PopoverTitle className="sr-only">帮助与反馈</PopoverTitle>
          <Tabs defaultValue="contact">
            <div className="border-b border-border px-1 pt-1">
              <TabsList className="w-full">
                <TabsTrigger value="contact" className="flex-1">
                  联系客服
                </TabsTrigger>
                <TabsTrigger value="feedback" className="flex-1">
                  意见反馈
                </TabsTrigger>
              </TabsList>
            </div>

            {/* Contact tab */}
            <TabsContent value="contact" className="flex flex-col items-center gap-3 p-4">
              <img
                src="/contact-qr.JPG"
                alt="企业微信客服二维码"
                className="h-[200px] w-[200px] rounded-md object-contain"
              />
              <p className="text-sm text-muted-foreground">微信扫码添加客服</p>
            </TabsContent>

            {/* Feedback tab */}
            <TabsContent value="feedback" className="flex flex-col gap-3 p-4">
              {/* Type selector */}
              <div className="flex gap-2">
                <Button
                  variant={type === 'bug' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setType('bug')}
                >
                  问题反馈
                </Button>
                <Button
                  variant={type === 'suggestion' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setType('suggestion')}
                >
                  功能建议
                </Button>
              </div>

              {/* Content */}
              <Textarea
                aria-label="反馈内容"
                placeholder="请描述您遇到的问题或想要的功能..."
                rows={4}
                value={content}
                onChange={(e) => setContent(e.target.value)}
                maxLength={1000}
              />

              {/* Submit */}
              <Button
                className="w-full"
                loading={isSubmitting}
                disabled={!content.trim()}
                onClick={handleSubmit}
              >
                提交反馈
              </Button>
            </TabsContent>
          </Tabs>
        </PopoverContent>
        {/* FAB button */}
        <PopoverTrigger render={<Button
          aria-label="帮助与反馈"
          variant="default"
          className="h-12 w-12 rounded-full shadow-lg"
        />}>
          <MessageCircle className="size-5" />
        </PopoverTrigger>
      </Popover>
    </div>
  )
}
