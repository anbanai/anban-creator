import { useRef } from 'react'
import { Input } from '@/components/ui/input'
import { Search, X } from 'lucide-react'

interface SearchInputProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  className?: string
  label?: string
}

export function SearchInput({ value, onChange, placeholder = '搜索...', className = '', label }: SearchInputProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  return (
    <div className={`relative ${className}`}>
      <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        ref={inputRef}
        type="search"
        aria-label={label || placeholder}
        className="bg-card pl-9 pr-10 [&::-webkit-search-cancel-button]:appearance-none"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {value && (
        <button
          type="button"
          aria-label="清空搜索"
          className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-r-lg text-muted-foreground hover:text-foreground"
          onClick={() => { onChange(''); inputRef.current?.focus() }}
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      )}
    </div>
  )
}
