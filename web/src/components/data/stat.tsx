import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Stat({ label, value, hint, tone, className }: { label: string; value: ReactNode; hint?: ReactNode; tone?: 'brand' | 'orange' | 'danger'; className?: string }) {
  return (
    <div className={cn('rounded-[20px] border bg-surface px-5 py-4', className)}>
      <p className="text-[13px] font-semibold text-muted-foreground">{label}</p>
      <p
        className={cn(
          'mt-2 font-display text-[30px] leading-none font-bold tracking-[-0.02em] tabular-nums',
          tone === 'brand' && 'text-brand-text',
          tone === 'orange' && 'text-orange-text',
          tone === 'danger' && 'text-danger',
        )}
      >
        {value}
      </p>
      {hint && <p className="mt-2.5 text-[12.5px] text-faint-foreground">{hint}</p>}
    </div>
  )
}

export function EmptyState({ icon, title, text, action }: { icon?: ReactNode; title: string; text?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
      {icon && <div className="mb-5 flex size-14 items-center justify-center rounded-[16px] bg-brand-soft text-brand-text">{icon}</div>}
      <p className="font-display text-[19px] font-bold tracking-[-0.01em]">{title}</p>
      {text && <p className="mt-2 max-w-[46ch] text-[14.5px] leading-relaxed text-muted-foreground">{text}</p>}
      {action && <div className="mt-6">{action}</div>}
    </div>
  )
}

/** Segmented control in the style of offerwall's portal switcher. */
export function Segmented<T extends string>({ value, onChange, options, className }: { value: T; onChange: (v: T) => void; options: { value: T; label: ReactNode }[]; className?: string }) {
  return (
    <div className={cn('inline-flex gap-0.5 rounded-[14px] bg-surface-2 p-1', className)} role="tablist">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="tab"
          aria-selected={o.value === value}
          onClick={() => onChange(o.value)}
          className={cn(
            'inline-flex h-9 items-center gap-2 rounded-[10px] px-3.5 text-[14px] font-semibold whitespace-nowrap transition-colors',
            o.value === value ? 'bg-surface text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
