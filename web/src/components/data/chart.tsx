import { cn } from '@/lib/utils'

/** Tiny trend line in de-emphasis grey with the current point in the brand colour. */
export function Sparkline({ points, max, className }: { points: number[]; max?: number; className?: string }) {
  const w = 120
  const h = 32
  const pad = 3
  if (points.length < 2) return <svg viewBox={`0 0 ${w} ${h}`} className={cn('h-8 w-[120px]', className)} aria-hidden />
  const top = Math.max(max ?? 0, ...points, 1)
  const xy = points.map((v, i) => [pad + (i * (w - pad * 2)) / (points.length - 1), h - pad - (v / top) * (h - pad * 2)] as const)
  const d = xy.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ')
  const [lx, ly] = xy[xy.length - 1]
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className={cn('h-8 w-[120px] shrink-0', className)} aria-hidden>
      <path d={d} fill="none" stroke="var(--faint-foreground)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" opacity="0.7" />
      <circle cx={lx} cy={ly} r="3.5" fill="var(--brand)" stroke="var(--surface)" strokeWidth="2" />
    </svg>
  )
}
