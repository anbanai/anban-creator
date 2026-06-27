import { Loader2 } from "lucide-react"
import * as React from "react"

import {
  Button as UIButton,
  buttonVariants,
} from "@/components/ui/button"

type UIButtonProps = React.ComponentProps<typeof UIButton>

interface ButtonProps extends UIButtonProps {
  loading?: boolean
}

function Button({
  loading = false,
  disabled,
  children,
  ...props
}: ButtonProps) {
  return (
    <UIButton disabled={disabled || loading} aria-busy={loading || undefined} {...props}>
      {loading && <Loader2 className="animate-spin" />}
      {children}
    </UIButton>
  )
}

export { Button, buttonVariants }
