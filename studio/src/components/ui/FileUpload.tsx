import * as React from "react"
import { X, ImageIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import http from "@/lib/http-client"

interface FileUploadProps {
  value?: string
  onChange?: (url: string) => void
  accept?: string
  maxSize?: number // in MB
  className?: string
}

export function FileUpload({
  value,
  onChange,
  accept = "image/jpeg,image/png,image/webp,image/gif",
  maxSize = 10,
  className,
}: FileUploadProps) {
  const [uploading, setUploading] = React.useState(false)
  const [error, setError] = React.useState("")
  const [isDragging, setIsDragging] = React.useState(false)
  const [showUrlInput, setShowUrlInput] = React.useState(false)
  const [urlInput, setUrlInput] = React.useState("")
  const fileInputRef = React.useRef<HTMLInputElement>(null)

  const uploadFile = async (file: File) => {
    if (file.size > maxSize * 1024 * 1024) {
      setError(`文件大小不能超过 ${maxSize}MB`)
      return
    }

    setError("")
    setUploading(true)

    try {
      const formData = new FormData()
      formData.append("file", file)

      const res = await http.post("/files/upload", formData, {
        headers: { "Content-Type": "multipart/form-data" },
      })

      // Response: { code: 0, msg: "success", data: { url, key, size, type } }
      const data = res.data as { data?: { url?: string } }
      const url = data.data?.url
      if (url) {
        onChange?.(url)
      }
    } catch (err: unknown) {
      const axiosErr = err as { response?: { data?: { msg?: string } } }
      const msg = axiosErr?.response?.data?.msg || "上传失败，请重试"
      setError(msg)
    } finally {
      setUploading(false)
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) uploadFile(file)
    e.target.value = ""
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = (e: React.DragEvent) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) {
      setIsDragging(false)
    }
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    const file = e.dataTransfer.files[0]
    if (file) uploadFile(file)
  }

  const clearImage = () => {
    onChange?.("")
    setError("")
  }

  const submitUrl = () => {
    const url = urlInput.trim()
    if (url) {
      onChange?.(url)
      setUrlInput("")
      setShowUrlInput(false)
    }
  }

  if (value) {
    return (
      <div className={cn("relative", className)}>
        <img
          src={value}
          alt="参考图预览"
          className="h-32 w-32 rounded-lg border object-cover"
          onError={(e) => {
            (e.target as HTMLImageElement).style.display = "none"
          }}
        />
        <button
          type="button"
          onClick={clearImage}
          className="absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full bg-destructive text-destructive-foreground shadow-sm transition-colors hover:bg-destructive/90"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
    )
  }

  return (
    <div className={cn("space-y-2", className)}>
      <div
        className={cn(
          "flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed bg-muted/30 px-4 py-6 transition-colors",
          isDragging
            ? "border-ring bg-muted/50"
            : "border-muted-foreground/25 hover:border-muted-foreground/50"
        )}
        onClick={() => !uploading && fileInputRef.current?.click()}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        {uploading ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
            上传中...
          </div>
        ) : (
          <>
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-muted">
              <ImageIcon className="h-5 w-5 text-muted-foreground" />
            </div>
            <div className="text-center text-sm text-muted-foreground">
              <span className="font-medium text-foreground">点击上传</span> 或拖拽图片到此处
            </div>
            <div className="text-xs text-muted-foreground/70">
              支持 JPG, PNG, WebP, GIF，最大 {maxSize}MB
            </div>
          </>
        )}
      </div>
      <input
        ref={fileInputRef}
        type="file"
        accept={accept}
        onChange={handleFileChange}
        className="hidden"
      />

      {!showUrlInput && !uploading && (
        <button
          type="button"
          className="text-xs text-muted-foreground transition-colors hover:text-foreground"
          onClick={() => setShowUrlInput(true)}
        >
          或粘贴图片链接
        </button>
      )}

      {showUrlInput && (
        <div className="flex gap-2">
          <input
            type="url"
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submitUrl()
            }}
            placeholder="https://..."
            className="h-8 flex-1 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus:border-ring focus:ring-3 focus:ring-ring/50"
          />
          <button
            type="button"
            onClick={submitUrl}
            className="h-8 rounded-lg bg-primary px-3 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            确认
          </button>
          <button
            type="button"
            onClick={() => {
              setShowUrlInput(false)
              setUrlInput("")
            }}
            className="h-8 rounded-lg px-2 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            取消
          </button>
        </div>
      )}

      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}
