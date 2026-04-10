import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { SHORTCUT_LIST } from '@/hooks/useKeyboardShortcuts'

interface ShortcutHelpProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export default function ShortcutHelp({ open, onOpenChange }: ShortcutHelpProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>键盘快捷键</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          {SHORTCUT_LIST.map((shortcut) => (
            <div key={shortcut.keys} className="flex items-center justify-between py-1.5">
              <span className="text-sm text-muted-foreground">{shortcut.description}</span>
              <kbd className="rounded border border-border bg-muted px-2 py-0.5 font-mono text-xs text-foreground">
                {shortcut.keys}
              </kbd>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
