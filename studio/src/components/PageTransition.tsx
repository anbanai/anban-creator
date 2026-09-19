import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'

export function PageTransition({ children }: { children: ReactNode }) {
  const location = useLocation()
  const reducedMotion = useReducedMotion()

  return (
    <AnimatePresence mode="wait">
      <motion.div
        key={location.pathname}
        initial={reducedMotion ? false : { opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        exit={reducedMotion ? { opacity: 1 } : { opacity: 0, y: -4 }}
        transition={{ duration: reducedMotion ? 0 : 0.15, ease: 'easeOut' }}
        className="mx-auto min-h-0 w-full max-w-[1440px] flex-1"
      >
        {children}
      </motion.div>
    </AnimatePresence>
  )
}
