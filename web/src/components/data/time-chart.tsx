import { type KeyboardEvent, type PointerEvent, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

// One series over time, with a hover layer: a crosshair that snaps to the nearest sample, a ringed
// focus dot and a tooltip that leads with the value. Arrow keys move the crosshair when focused.
// One axis only; two measures get two charts. A visually hidden table carries the same numbers.

export type TimePoint = { t: number; v: number }

/** Clean tick values (0, 25, 50, …) covering [0, max]. */
export function niceTicks(max: number, count = 4) {
  if (max <= 0) return [0]
  const raw = max / count
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw
  const ticks: number[] = []
  for (let v = 0; v <= max + step * 0.001; v += step) ticks.push(v)
  if (ticks[ticks.length - 1] < max) ticks.push(ticks[ticks.length - 1] + step)
  return ticks
}

function timeLabel(t: number, spanMs: number) {
  const d = new Date(t)
  if (spanMs > 2 * 86400_000) return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

function tooltipTime(t: number) {
  return new Date(t).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function TimeChart({
  points,
  format,
  max,
  height = 180,
  label,
  color = 'var(--chart-1)',
  empty = 'Not enough data yet. A point is recorded every minute.',
  bytes = false,
  limit,
}: {
  points: TimePoint[]
  format: (v: number) => string
  /** Pin the top of the scale (e.g. the bot's limit) instead of fitting the data. */
  max?: number
  height?: number
  label: string
  color?: string
  empty?: string
  /** Values are bytes: ticks step in whole MiB/GiB so the axis reads 16 MB, 32 MB, … */
  bytes?: boolean
  /** Draw a labelled reference line, e.g. the bot's memory limit. */
  limit?: { value: number; label: string }
}) {
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [active, setActive] = useState<number | null>(null)

  const hasData = points.length >= 2
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    setWidth(el.getBoundingClientRect().width)
    const ro = new ResizeObserver(([e]) => setWidth(e.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [hasData])

  const padL = 52
  const padR = 12
  const padT = 10
  const padB = 26
  const plotW = Math.max(width - padL - padR, 10)
  const plotH = height - padT - padB

  const geo = useMemo(() => {
    if (points.length < 2) return null
    const dataMax = Math.max(...points.map((p) => p.v))
    const top0 = Math.max(max ?? 0, limit?.value ?? 0, dataMax, 1e-9)
    const unit = bytes ? (top0 >= 2 ** 30 ? 2 ** 30 : top0 >= 2 ** 20 ? 2 ** 20 : 1) : 1
    const ticks = niceTicks(top0 / unit).map((t) => t * unit)
    const top = ticks[ticks.length - 1]
    const t0 = points[0].t
    const t1 = points[points.length - 1].t
    const x = (t: number) => padL + ((t - t0) / Math.max(t1 - t0, 1)) * plotW
    const y = (v: number) => padT + plotH - (v / top) * plotH
    const line = points.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)} ${y(p.v).toFixed(1)}`).join(' ')
    const area = `${line} L${x(t1).toFixed(1)} ${padT + plotH} L${x(t0).toFixed(1)} ${padT + plotH} Z`
    return { ticks, x, y, line, area, t0, t1 }
  }, [points, max, plotW, plotH, bytes, limit?.value])

  // The measured box is always rendered so its size is known before data arrives.
  if (!geo)
    return (
      <div ref={box} className="flex items-center justify-center rounded-[14px] bg-surface-2/60 px-6 text-center text-[13px] text-faint-foreground" style={{ height }}>
        {empty}
      </div>
    )

  const nearest = (px: number) => {
    // Points are time-ordered: binary search for the closest sample to the pointer.
    const t = geo.t0 + ((px - padL) / plotW) * (geo.t1 - geo.t0)
    let lo = 0
    let hi = points.length - 1
    while (hi - lo > 1) {
      const mid = (lo + hi) >> 1
      if (points[mid].t < t) lo = mid
      else hi = mid
    }
    return t - points[lo].t < points[hi].t - t ? lo : hi
  }
  const onMove = (e: PointerEvent<SVGRectElement>) => {
    const rect = e.currentTarget.ownerSVGElement!.getBoundingClientRect()
    setActive(nearest(e.clientX - rect.left))
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      const step = e.shiftKey ? 10 : 1
      setActive((a) => Math.max(0, Math.min(points.length - 1, (a ?? points.length - 1) + (e.key === 'ArrowLeft' ? -step : step))))
    } else if (e.key === 'Escape') setActive(null)
  }

  const a = active != null ? points[active] : null
  const ax = a ? geo.x(a.t) : 0
  const ay = a ? geo.y(a.v) : 0
  const span = geo.t1 - geo.t0
  const xTicks = [geo.t0, geo.t0 + span / 2, geo.t1]
  const flip = ax > width - 170
  const last = points[points.length - 1]

  return (
    <div
      ref={box}
      className="relative outline-none focus-visible:rounded-[12px] focus-visible:ring-4 focus-visible:ring-brand-soft"
      tabIndex={0}
      role="img"
      aria-label={`${label}: ${points.length} samples, latest ${format(last.v)}, peak ${format(Math.max(...points.map((p) => p.v)))}. Use arrow keys to read values.`}
      onKeyDown={onKey}
      onBlur={() => setActive(null)}
    >
      {width > 0 && (
        <svg width={width} height={height} className="block overflow-visible">
          {geo.ticks.map((v) => (
            <g key={v}>
              <line x1={padL} x2={width - padR} y1={geo.y(v)} y2={geo.y(v)} stroke="var(--chart-grid)" strokeWidth={1} />
              <text x={padL - 10} y={geo.y(v)} dy="0.32em" textAnchor="end" className="fill-faint-foreground text-[11px] tabular-nums">
                {format(v)}
              </text>
            </g>
          ))}
          {xTicks.map((t, i) => (
            <text
              key={i}
              x={geo.x(t)}
              y={height - 6}
              textAnchor={i === 0 ? 'start' : i === 2 ? 'end' : 'middle'}
              className="fill-faint-foreground text-[11px] tabular-nums"
            >
              {timeLabel(t, span)}
            </text>
          ))}
          {limit && (
            <g>
              <line x1={padL} x2={width - padR} y1={geo.y(limit.value)} y2={geo.y(limit.value)} stroke="var(--muted-foreground)" strokeWidth={1} strokeDasharray="4 4" opacity={0.6} />
              <text x={width - padR} y={geo.y(limit.value) - 6} textAnchor="end" className="fill-muted-foreground text-[11px] font-semibold">
                {limit.label}
              </text>
            </g>
          )}
          <path d={geo.area} fill={color} opacity={0.1} />
          <path d={geo.line} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          {!a && <circle cx={geo.x(last.t)} cy={geo.y(last.v)} r={4} fill={color} stroke="var(--surface)" strokeWidth={2} />}
          {a && (
            <>
              <line x1={ax} x2={ax} y1={padT} y2={padT + plotH} stroke="var(--muted-foreground)" strokeWidth={1} opacity={0.5} />
              <circle cx={ax} cy={ay} r={5} fill={color} stroke="var(--surface)" strokeWidth={2} />
            </>
          )}
          {/* The hit area is the whole plot, so the reader aims at a time, not at a 2px line. */}
          <rect
            x={padL}
            y={padT}
            width={plotW}
            height={plotH}
            fill="transparent"
            style={{ cursor: 'crosshair' }}
            onPointerMove={onMove}
            onPointerDown={onMove}
            onPointerLeave={() => setActive(null)}
          />
        </svg>
      )}
      {a && (
        <div
          className="pointer-events-none absolute z-10 min-w-[132px] rounded-[12px] border bg-popover px-3 py-2 shadow-[0_12px_30px_-12px_oklch(0_0_0/0.45)]"
          style={{ left: flip ? ax - 14 : ax + 14, top: Math.max(0, Math.min(ay - 28, height - 64)), transform: flip ? 'translateX(-100%)' : undefined }}
        >
          <p className="font-display text-[17px] leading-tight font-bold tabular-nums">{format(a.v)}</p>
          <p className="mt-1 flex items-center gap-1.5 text-[12px] text-muted-foreground">
            <span className="inline-block h-0.5 w-3 rounded-full" style={{ background: color }} />
            {label} · {tooltipTime(a.t)}
          </p>
        </div>
      )}
      <table className="sr-only">
        <caption>{label}</caption>
        <tbody>
          {points.map((p) => (
            <tr key={p.t}>
              <th scope="row">{tooltipTime(p.t)}</th>
              <td>{format(p.v)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** Shared tooltip chrome for charts that hover per mark. */
export function ChartTooltip({ x, y, children, flip }: { x: number; y: number; children: React.ReactNode; flip?: boolean }) {
  return (
    <div
      className={cn('pointer-events-none absolute z-10 min-w-[140px] rounded-[12px] border bg-popover px-3 py-2 shadow-[0_12px_30px_-12px_oklch(0_0_0/0.45)]')}
      style={{ left: x, top: y, transform: `translate(${flip ? '-100%' : '0'}, -100%)` }}
    >
      {children}
    </div>
  )
}
