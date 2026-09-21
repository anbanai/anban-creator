import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

type Props = {
  fileName?: string
  pending: boolean
  error: Error | null
  onCancel: () => void
  onConfirm: () => void
}

export default function RevokeImportDialog({ fileName, pending, error, onCancel, onConfirm }: Props) {
  return <Dialog open={Boolean(fileName)} onOpenChange={(open) => { if (!open && !pending) onCancel() }}>
    <DialogContent closeButtonDisabled={pending}>
      <DialogHeader>
        <DialogTitle>撤销本次导入</DialogTitle>
        <DialogDescription>请确认要撤销的文件及影响。</DialogDescription>
      </DialogHeader>
      <p className="break-all font-medium">{fileName}</p>
      <p className="text-sm text-muted-foreground">撤销后，本批次数据不再计入统计，原始文件和导入历史仍会保留。其他批次、官方接口数据和已发布内容不受影响。</p>
      <p className="text-sm text-muted-foreground">如需更正，请修改文件后重新导入。</p>
      {error && <Alert variant="destructive"><AlertTitle>撤销失败</AlertTitle><AlertDescription>{error.message}</AlertDescription></Alert>}
      <DialogFooter>
        <Button variant="outline" disabled={pending} onClick={onCancel}>取消</Button>
        <Button variant="destructive" disabled={pending} onClick={onConfirm}>{pending ? '正在撤销…' : '确认撤销'}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
