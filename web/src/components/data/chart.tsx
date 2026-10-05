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

/** Area chart for a metric over time. Values are plotted against `max` when given. */
export function AreaChart({ points, max, format, height = 160 }: { points: { t: number; v: number }[]; max?: number; format: (v: number) => string; height?: number }) {
  const w = 600
  const h = height
  const padY = 10
  if (points.length < 2)
    return (
      <div className="flex items-center justify-center rounded-[14px] bg-surface-2/60 text-[13px] text-faint-foreground" style={{ height }}>
        Not enough data yet. A point is recorded every minute.
      </div>
    )
  const top = Math.max(max ?? 0, ...points.map((p) => p.v), 1e-9)
  const t0 = points[0].t
  const t1 = points[points.length - 1].t
  const x = (t: number) => ((t - t0) / Math.max(t1 - t0, 1)) * w
  const y = (v: number) => h - padY - (v / top) * (h - padY * 2)
  const line = points.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)} ${y(p.v).toFixed(1)}`).join(' ')
  const area = `${line} L${w} ${h} L0 ${h} Z`
  const peak = Math.max(...points.map((p) => p.v))
  return (
    <div>
      <svg viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" className="w-full" style={{ height }} aria-hidden>
        {[0.25, 0.5, 0.75].map((f) => (
          <line key={f} x1="0" x2={w} y1={h * f} y2={h * f} stroke="var(--border)" strokeDasharray="3 5" vectorEffect="non-scaling-stroke" />
        ))}
        <path d={area} fill="var(--brand)" opacity="0.12" />
        <path d={line} fill="none" stroke="var(--brand)" strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
      </svg>
      <div className="mt-2 flex justify-between text-[12px] text-faint-foreground tabular-nums">
        <span>{new Date(t0).toLocaleString(undefined, { hour: '2-digit', minute: '2-digit', month: 'short', day: 'numeric' })}</span>
        <span>peak {format(peak)}</span>
        <span>{new Date(t1).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}</span>
      </div>
    </div>
  )
}
