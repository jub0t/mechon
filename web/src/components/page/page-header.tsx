import type { ReactNode } from 'react'

export function PageHeader({ eyebrow, title, description, actions }: { eyebrow?: ReactNode; title: ReactNode; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-10 flex flex-wrap items-end justify-between gap-4">
      <div>
        {eyebrow && <p className="mb-3 text-[12px] font-semibold tracking-[0.08em] text-faint-foreground uppercase">{eyebrow}</p>}
        <h1 className="text-title text-[clamp(1.9rem,1.3rem+1.6vw,2.6rem)]">{title}</h1>
        {description && <p className="mt-3 max-w-[60ch] text-[15.5px] leading-relaxed text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}
