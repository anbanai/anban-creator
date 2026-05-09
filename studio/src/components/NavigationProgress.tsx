import { motion, useAnimation } from 'motion/react'
import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'

export function NavigationProgress() {
  const location = useLocation()
  const controls = useAnimation()

  useEffect(() => {
    controls.set({ scaleX: 0, opacity: 1 })
    controls.start({
      scaleX: 0.3,
      opacity: 1,
      transition: { duration: 0.2, ease: 'easeOut' },
    })

    const timer = setTimeout(() => {
      controls.start({
        scaleX: 1,
        opacity: 0,
        transition: { duration: 0.3, ease: 'easeIn' },
      })
    }, 200)

    return () => clearTimeout(timer)
  }, [location.pathname, controls])

  return (
    <motion.div
      className="fixed top-0 left-0 right-0 z-50 h-0.5 origin-left bg-primary"
      initial={{ scaleX: 0, opacity: 0 }}
      animate={controls}
    />
  )
}
