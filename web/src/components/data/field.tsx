import type { ReactNode } from 'react'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

export function Field({ label, htmlFor, hint, children, className, aside }: { label: string; htmlFor?: string; hint?: ReactNode; children: ReactNode; className?: string; aside?: ReactNode }) {
  return (
    <div className={cn('space-y-2', className)}>
      <div className="flex items-baseline justify-between gap-3">
        <Label htmlFor={htmlFor} className="text-[13.5px] font-semibold">
          {label}
        </Label>
        {aside}
      </div>
      {children}
      {hint && <p className="text-[12.5px] leading-relaxed text-faint-foreground">{hint}</p>}
    </div>
  )
}

export function FormError({ error }: { error: unknown }) {
  if (!error) return null
  const message = error instanceof Error ? error.message : 'Something went wrong.'
  return <p className="rounded-[12px] bg-danger-soft px-3.5 py-2.5 text-[14px] font-medium text-danger">{message}</p>
}
