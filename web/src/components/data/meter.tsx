import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'

/** A labelled usage bar. Turns orange past 80% and red past 95%. */
export function Meter({ label, value, max, display, className }: { label: string; value: number; max: number; display?: string; className?: string }) {
  const reduce = useReducedMotion()
  const ratio = max > 0 ? Math.min(value / max, 1) : 0
  const color = ratio > 0.95 ? 'bg-danger' : ratio > 0.8 ? 'bg-orange' : 'bg-brand'
  return (
    <div className={className}>
      <div className="flex items-baseline justify-between gap-3 text-[13px]">
        <span className="font-semibold text-muted-foreground">{label}</span>
        <span className="font-mono text-[12.5px] text-foreground tabular-nums">{display}</span>
      </div>
      <div className="mt-2 h-2 overflow-hidden rounded-full bg-surface-2">
        <motion.div
          className={cn('h-full rounded-full', color)}
          initial={reduce ? false : { width: 0 }}
          animate={{ width: `${Math.max(ratio * 100, ratio > 0 ? 2 : 0)}%` }}
          transition={{ duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
        />
      </div>
    </div>
  )
}

/** A compact inline bar for table cells. */
export function MiniBar({ value, max }: { value: number; max: number }) {
  const ratio = max > 0 ? Math.min(value / max, 1) : 0
  const color = ratio > 0.95 ? 'bg-danger' : ratio > 0.8 ? 'bg-orange' : 'bg-brand'
  return (
    <div className="h-1.5 w-full min-w-16 overflow-hidden rounded-full bg-surface-2">
      <div className={cn('h-full rounded-full transition-[width] duration-500', color)} style={{ width: `${ratio * 100}%` }} />
    </div>
  )
}
