import { cn } from '@/lib/utils'

/** Three slats in a rounded tile: bots standing in their own lanes. */
export function Mark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={cn('size-8 shrink-0', className)} aria-hidden>
      <rect width="32" height="32" rx="9" className="fill-brand" />
      <rect x="7" y="9" width="4.5" height="14" rx="2.25" fill="white" />
      <rect x="13.75" y="13" width="4.5" height="10" rx="2.25" fill="white" opacity=".7" />
      <rect x="20.5" y="9" width="4.5" height="14" rx="2.25" fill="white" />
    </svg>
  )
}

export function Logo({ className }: { className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2.5', className)}>
      <Mark className="size-7" />
      <span className="font-display text-[19px] font-bold tracking-[-0.02em]">Mechon</span>
    </span>
  )
}
