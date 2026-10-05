import { useState } from 'react'
import { cn } from '@/lib/utils'
import { ChartTooltip, niceTicks } from './time-chart'

// ---------- One stacked bar: parts of a whole ----------

export type Part = { key: string; label: string; value: number; color: string }

/** A single 100% bar split into parts, with a legend that carries labels and counts so identity
 *  never rests on colour alone. Each segment has its own hover/focus tooltip. */
export function StackedBar({ parts, total, unit }: { parts: Part[]; total: number; unit: [string, string] }) {
  const [active, setActive] = useState<string | null>(null)
  const shown = parts.filter((p) => p.value > 0)
  if (total === 0) return <div className="h-4 rounded-full bg-surface-2" aria-label="Nothing to show yet" />
  const plural = (n: number) => (n === 1 ? unit[0] : unit[1])
  return (
    <div>
      <div className="flex h-4 gap-[2px]" role="list" aria-label={`${total} ${plural(total)} by state`}>
        {shown.map((p, i) => {
          const pct = (p.value / total) * 100
          const on = active === p.key
          return (
            <div
              key={p.key}
              role="listitem"
              tabIndex={0}
              aria-label={`${p.label}: ${p.value} ${plural(p.value)}, ${Math.round(pct)}%`}
              onPointerEnter={() => setActive(p.key)}
              onPointerLeave={() => setActive(null)}
              onFocus={() => setActive(p.key)}
              onBlur={() => setActive(null)}
              className="relative -my-2 py-2 outline-none"
              style={{ width: `${pct}%`, minWidth: 6 }}
            >
              <div
                className={cn('h-full transition-[filter,transform] duration-150', i === 0 && 'rounded-l-full', i === shown.length - 1 && 'rounded-r-full', on && 'brightness-125')}
                style={{ background: p.color }}
              />
              {on && (
                <ChartTooltip x={0} y={-4}>
                  <p className="font-display text-[17px] leading-tight font-bold tabular-nums">
                    {p.value} <span className="text-[13px] font-semibold text-muted-foreground">{plural(p.value)}</span>
                  </p>
                  <p className="mt-1 text-[12px] text-muted-foreground">
                    {p.label} · {Math.round(pct)}%
                  </p>
                </ChartTooltip>
              )}
            </div>
          )
        })}
      </div>
      <ul className="mt-4 flex flex-wrap gap-x-5 gap-y-2">
        {parts.map((p) => (
          <li
            key={p.key}
            className={cn('flex items-center gap-2 text-[13px] transition-opacity', active && active !== p.key && 'opacity-45')}
            onPointerEnter={() => p.value > 0 && setActive(p.key)}
            onPointerLeave={() => setActive(null)}
          >
            <span className="size-2.5 rounded-[3px]" style={{ background: p.color }} />
            <span className="text-muted-foreground">{p.label}</span>
            <span className="font-semibold tabular-nums">{p.value}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// ---------- Columns over days, two stacked series ----------

export type DayColumn = { day: number; a: number; b: number }

/** Stacked columns per day. Columns are capped at 24px with a rounded top and a square base; the
 *  whole day's band is the hover target and the tooltip lists both series. */
export function DayColumns({ days, series, height = 170 }: { days: DayColumn[]; series: [{ label: string; color: string }, { label: string; color: string }]; height?: number }) {
  const [active, setActive] = useState<number | null>(null)
  const padT = 8
  const padB = 24
  const plotH = height - padT - padB
  const max = Math.max(1, ...days.map((d) => d.a + d.b))
  const ticks = niceTicks(max, 3).filter((t) => Number.isInteger(t))
  const top = ticks[ticks.length - 1]
  const y = (v: number) => (v / top) * plotH
  const totalA = days.reduce((s, d) => s + d.a, 0)
  const totalB = days.reduce((s, d) => s + d.b, 0)

  return (
    <div>
      <ul className="mb-3 flex flex-wrap gap-x-5 gap-y-1.5 text-[13px]">
        {series.map((s, i) => (
          <li key={s.label} className="flex items-center gap-2">
            <span className="size-2.5 rounded-[3px]" style={{ background: s.color }} />
            <span className="text-muted-foreground">{s.label}</span>
            <span className="font-semibold tabular-nums">{i === 0 ? totalA : totalB}</span>
          </li>
        ))}
      </ul>
      <div className="relative flex" style={{ height }}>
        <div className="relative w-8 shrink-0">
          {ticks.map((t) => (
            <span key={t} className="absolute right-2 -translate-y-1/2 text-[11px] text-faint-foreground tabular-nums" style={{ top: padT + plotH - y(t) }}>
              {t}
            </span>
          ))}
        </div>
        <div className="relative flex-1">
          {ticks.map((t) => (
            <div key={t} className="absolute inset-x-0 h-px bg-[var(--chart-grid)]" style={{ top: padT + plotH - y(t) }} />
          ))}
          <div className="absolute inset-x-0 flex" style={{ top: padT, height: plotH }}>
            {days.map((d, i) => {
              const on = active === i
              const date = new Date(d.day)
              const name = date.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
              return (
                <div
                  key={d.day}
                  tabIndex={0}
                  aria-label={`${name}: ${d.a} ${series[0].label.toLowerCase()}, ${d.b} ${series[1].label.toLowerCase()}`}
                  className={cn('relative flex flex-1 flex-col items-center justify-end outline-none', on && 'bg-foreground/[0.04]')}
                  onPointerEnter={() => setActive(i)}
                  onPointerLeave={() => setActive(null)}
                  onFocus={() => setActive(i)}
                  onBlur={() => setActive(null)}
                >
                  <div className={cn('flex w-full max-w-6 flex-col-reverse gap-[2px] transition-[filter]', on && 'brightness-125')}>
                    {d.a > 0 && <div style={{ height: y(d.a), background: series[0].color }} className={cn(d.b === 0 && 'rounded-t-[4px]')} />}
                    {d.b > 0 && <div style={{ height: y(d.b), background: series[1].color }} className="rounded-t-[4px]" />}
                  </div>
                  <span className={cn('absolute -bottom-5 text-[11px] text-faint-foreground tabular-nums', i % 2 && days.length > 8 && 'hidden sm:block')}>
                    {date.getDate()}
                  </span>
                  {on && (
                    <ChartTooltip x={0} y={-6} flip={i > days.length * 0.6}>
                      <p className="mb-1.5 text-[12px] font-semibold text-muted-foreground">{name}</p>
                      {[d.a, d.b].map((v, j) => (
                        <p key={j} className="flex items-center justify-between gap-4 text-[12.5px]">
                          <span className="flex items-center gap-1.5 text-muted-foreground">
                            <span className="inline-block h-0.5 w-3 rounded-full" style={{ background: series[j].color }} />
                            {series[j].label}
                          </span>
                          <span className="font-display text-[15px] font-bold tabular-nums">{v}</span>
                        </p>
                      ))}
                    </ChartTooltip>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      </div>
    </div>
  )
}
