import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Card({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('rounded-[20px] border bg-surface', className)} {...props} />
}

export function CardHeader({ title, aside, className }: { title: ReactNode; aside?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex items-center justify-between gap-4 px-6 pt-5 pb-3', className)}>
      <h2 className="font-display text-[17px] font-bold tracking-[-0.01em]">{title}</h2>
      {aside}
    </div>
  )
}

/** A live status dot. `pulse` animates it to say "this is being watched right now". */
export function StatusDot({ tone, pulse }: { tone: 'success' | 'danger' | 'neutral' | 'orange'; pulse?: boolean }) {
  const color = { success: 'bg-success', danger: 'bg-danger', neutral: 'bg-faint-foreground', orange: 'bg-orange' }[tone]
  return (
    <span className="relative inline-flex size-2.5 shrink-0">
      {pulse && <span className={cn('absolute inset-0 animate-ping rounded-full opacity-50', color)} />}
      <span className={cn('relative inline-flex size-2.5 rounded-full', color)} />
    </span>
  )
}
