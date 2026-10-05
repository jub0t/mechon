import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Check, ChevronDown, Layers, Rocket, Server, ShieldCheck, Users } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { type DayColumn, DayColumns, StackedBar } from '@/components/data/bars'
import { Meter } from '@/components/data/meter'
import { Segmented, Stat } from '@/components/data/stat'
import { TimeChart } from '@/components/data/time-chart'
import { Reveal } from '@/components/motion/reveal'
import { Card, StatusDot } from '@/components/page/card'
import { api, type Overview, type User } from '@/lib/api'
import { bytes, mb } from '@/lib/format'
import { cn } from '@/lib/utils'

// The admin dashboard. Order is deliberate: the numbers and the first charts sit above the fold;
// the setup checklist is a horizontal strip that folds to one line, so it never pushes them down.

function greeting(d = new Date()) {
  const h = d.getHours()
  return h < 5 ? 'Up late' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
}

const today = () => new Date().toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })
const firstName = (name: string) => name.trim().split(/\s+/)[0] || name
const bytesShort = (v: number) => (v === 0 ? '0' : bytes(v).replace('.0 ', ' '))

type Range = '1h' | '24h' | '7d'

export function OverviewPage({ user }: { user: User }) {
  const o = useQuery({ queryKey: ['overview'], queryFn: api.overview, refetchInterval: 10000 })
  const [range, setRange] = useState<Range>('24h')
  const d = o.data
  const running = d?.botsByState.running ?? 0
  const crashed = d?.botsByState.crashed ?? 0

  return (
    <>
      <Reveal>
        <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="mb-2 text-[12px] font-semibold tracking-[0.08em] text-faint-foreground uppercase">{today()}</p>
            <h1 className="text-title text-[clamp(1.7rem,1.3rem+1.1vw,2.2rem)]">
              {greeting()}, {firstName(user.name)}.
            </h1>
          </div>
          <Segmented
            value={range}
            onChange={setRange}
            options={[
              { value: '1h', label: 'Hour' },
              { value: '24h', label: 'Day' },
              { value: '7d', label: 'Week' },
            ]}
          />
        </div>
      </Reveal>

      <Reveal delay={0.03}>
        <SetupStrip d={d} />
      </Reveal>

      <Reveal delay={0.06}>
        <div className="mb-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <Stat label="Bots running" value={d ? `${running}/${d.bots}` : '…'} hint={crashed ? `${crashed} crashed` : 'none crashed'} tone={crashed ? 'danger' : undefined} />
          <Stat label="Nodes online" value={d ? `${d.nodesOnline}/${d.nodes}` : '…'} hint={d && d.nodes > d.nodesOnline ? 'some are offline' : 'all connected'} tone={d && d.nodes > d.nodesOnline ? 'orange' : undefined} />
          <Stat label="Customers" value={d?.users ?? '…'} hint={`${d?.plans ?? 0} ${d?.plans === 1 ? 'plan' : 'plans'} on sale`} />
          <Stat
            label="Memory sold"
            value={d ? (d.capacity.memoryMb ? `${Math.round((d.allocated.memoryMb / d.capacity.memoryMb) * 100)}%` : '0%') : '…'}
            hint={d ? `${mb(d.allocated.memoryMb)} of ${mb(d.capacity.memoryMb)}` : ''}
          />
        </div>
      </Reveal>

      <Reveal delay={0.09}>
        <Activity range={range} d={d} />
      </Reveal>
    </>
  )
}

// ---------- Setup ----------

type Step = { icon: typeof Server; title: string; text: string; done: boolean; href: string; cta: string }

function stepsFor(d?: Overview): Step[] {
  return [
    { icon: ShieldCheck, title: 'Admin account', text: 'That is you. Signed in and ready.', done: true, href: '/account', cta: 'Account' },
    { icon: Server, title: 'Add a node', text: 'Run the agent on a server with Docker. No ports to open.', done: (d?.nodes ?? 0) > 0, href: '/nodes?new=1', cta: 'Add node' },
    { icon: Layers, title: 'Create a plan', text: 'The bots, memory, CPU and disk you sell.', done: (d?.plans ?? 0) > 0, href: '/plans?new=1', cta: 'New plan' },
    { icon: Users, title: 'Add a user', text: 'By hand, or from your billing system.', done: (d?.users ?? 0) > 0, href: '/users?new=1', cta: 'New user' },
    { icon: Rocket, title: 'Deploy a bot', text: 'discord.js, discord.py or Bun.', done: (d?.bots ?? 0) > 0, href: '/bots?new=1', cta: 'New bot' },
  ]
}

function readOpen(fallback: boolean) {
  try {
    const saved = localStorage.getItem('mechon-setup-open')
    if (saved != null) return saved === '1'
  } catch {
    // storage blocked: use the default
  }
  return fallback
}

function SetupStrip({ d }: { d?: Overview }) {
  const steps = stepsFor(d)
  const done = steps.filter((s) => s.done).length
  const complete = done === steps.length
  const next = steps.find((s) => !s.done)
  const reduce = useReducedMotion()
  const [open, setOpen] = useState(() => readOpen(true))
  const isOpen = d ? open && !(complete && localStorageUnset()) : false
  const toggle = () => {
    const nextOpen = !isOpen
    setOpen(nextOpen)
    try {
      localStorage.setItem('mechon-setup-open', nextOpen ? '1' : '0')
    } catch {
      // the toggle still works for this visit
    }
  }

  return (
    <Card className="mb-5 overflow-hidden">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3.5">
        <button type="button" onClick={toggle} aria-expanded={isOpen} aria-controls="setup-steps" className="flex min-w-0 flex-1 items-center gap-3 text-left">
          <ProgressRing value={done / steps.length} done={complete} />
          <span className="min-w-0">
            <span className="block text-[14.5px] font-semibold">{complete ? 'Your host is set up' : 'Get your host ready'}</span>
            <span className="block text-[12.5px] text-muted-foreground tabular-nums">
              {done} of {steps.length} done{next && !isOpen ? ` · next: ${next.title.toLowerCase()}` : ''}
            </span>
          </span>
        </button>
        {next && !isOpen && (
          <Link
            to={next.href}
            className="inline-flex h-9 items-center gap-1.5 rounded-full bg-foreground px-4 text-[13.5px] font-semibold text-background transition-opacity hover:opacity-90"
          >
            {next.cta} <ArrowRight className="size-4" strokeWidth={2.25} />
          </Link>
        )}
        <button
          type="button"
          onClick={toggle}
          aria-label={isOpen ? 'Collapse setup' : 'Expand setup'}
          className="inline-flex size-9 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
        >
          <ChevronDown className={cn('size-[18px] transition-transform duration-200', isOpen && 'rotate-180')} />
        </button>
      </div>
      <AnimatePresence initial={false}>
        {isOpen && (
          <motion.ol
            id="setup-steps"
            className="grid overflow-hidden border-t sm:grid-cols-2 lg:grid-cols-5"
            initial={reduce ? false : { height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={reduce ? undefined : { height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
          >
            {steps.map((s, i) => (
              <li key={s.title} className="border-b sm:[&:nth-child(odd)]:border-r lg:border-r lg:border-b-0 lg:last:border-r-0">
                <Link to={s.href} className="group flex h-full flex-col gap-3 px-5 py-4 transition-colors hover:bg-surface-2/60">
                  <span className="flex items-center justify-between">
                    <span
                      className={cn(
                        'flex size-9 items-center justify-center rounded-[11px] transition-colors',
                        s.done ? 'bg-brand text-brand-foreground' : 'bg-surface-2 text-muted-foreground group-hover:bg-brand-soft group-hover:text-brand-text',
                      )}
                    >
                      {s.done ? <Check className="size-4" strokeWidth={2.75} /> : <s.icon className="size-4" strokeWidth={2.25} />}
                    </span>
                    <span className="text-[12px] font-semibold text-faint-foreground tabular-nums">{i + 1}</span>
                  </span>
                  <span>
                    <span className={cn('block text-[14px] font-semibold', s.done && 'text-muted-foreground')}>{s.title}</span>
                    <span className="mt-0.5 block text-[12.5px] leading-snug text-muted-foreground">{s.text}</span>
                  </span>
                  <span className={cn('mt-auto inline-flex items-center gap-1 text-[12.5px] font-semibold', s.done ? 'text-brand-text' : 'text-foreground')}>
                    {s.done ? 'Done' : s.cta}
                    {!s.done && <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />}
                  </span>
                </Link>
              </li>
            ))}
          </motion.ol>
        )}
      </AnimatePresence>
    </Card>
  )
}

/** A completed setup starts folded unless the admin opened it on purpose. */
function localStorageUnset() {
  try {
    return localStorage.getItem('mechon-setup-open') == null
  } catch {
    return true
  }
}

function ProgressRing({ value, done }: { value: number; done: boolean }) {
  const r = 15
  const c = 2 * Math.PI * r
  return (
    <span className="relative inline-flex size-10 shrink-0 items-center justify-center" aria-hidden>
      <svg viewBox="0 0 36 36" className="size-10 -rotate-90">
        <circle cx="18" cy="18" r={r} fill="none" stroke="var(--surface-2)" strokeWidth="3.5" />
        <circle cx="18" cy="18" r={r} fill="none" stroke="var(--brand)" strokeWidth="3.5" strokeLinecap="round" strokeDasharray={`${value * c} ${c}`} className="transition-[stroke-dasharray] duration-700" />
      </svg>
      {done && <Check className="absolute size-4 text-brand-text" strokeWidth={3} />}
    </span>
  )
}

// ---------- Activity ----------

function Activity({ range, d }: { range: Range; d?: Overview }) {
  const a = useQuery({ queryKey: ['activity', range], queryFn: () => api.activity(range), refetchInterval: 60_000, placeholderData: (prev) => prev })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes, refetchInterval: 15_000 })
  const fleet = a.data?.fleet ?? []
  const days = useMemo(() => {
    const byDay = new Map((a.data?.deploys ?? []).map((x) => [new Date(x.day).toDateString(), x]))
    const out: DayColumn[] = []
    for (let i = 13; i >= 0; i--) {
      const day = new Date()
      day.setHours(0, 0, 0, 0)
      day.setDate(day.getDate() - i)
      const hit = byDay.get(day.toDateString())
      out.push({ day: day.getTime(), a: hit?.succeeded ?? 0, b: hit?.failed ?? 0 })
    }
    return out
  }, [a.data])
  const dim = a.isFetching && a.isPlaceholderData

  return (
    <div className={cn('space-y-5 transition-opacity', dim && 'opacity-60')}>
      <div className="grid gap-5 lg:grid-cols-3">
        <Card className="px-6 pt-5 pb-4 lg:col-span-2">
          <ChartHead title="Memory in use" value={fleet.length ? bytes(fleet[fleet.length - 1].memory) : undefined} hint="All bots, every minute" />
          <TimeChart label="Memory in use" bytes height={230} points={fleet.map((p) => ({ t: p.t, v: p.memory }))} format={bytesShort} />
        </Card>
        <div className="flex flex-col gap-5">
          <Card className="px-6 py-5">
            <ChartHead title="Bots by state" value={String(d?.bots ?? 0)} hint="Right now" />
            <StackedBar
              total={d?.bots ?? 0}
              unit={['bot', 'bots']}
              parts={[
                { key: 'running', label: 'Running', value: d?.botsByState.running ?? 0, color: 'var(--success)' },
                { key: 'installing', label: 'Installing', value: d?.botsByState.installing ?? 0, color: 'var(--orange)' },
                { key: 'crashed', label: 'Crashed', value: d?.botsByState.crashed ?? 0, color: 'var(--danger)' },
                { key: 'stopped', label: 'Stopped', value: (d?.botsByState.stopped ?? 0) + (d?.botsByState.pending ?? 0), color: 'var(--faint-foreground)' },
                { key: 'unknown', label: 'Node offline', value: d?.botsByState.unknown ?? 0, color: 'var(--surface-2)' },
              ]}
            />
          </Card>
          <Card className="flex-1 px-6 py-5">
            <ChartHead title="Capacity by node" hint="Memory sold of what each node sells" />
            {nodes.data?.length ? (
              <div className="space-y-4">
                {nodes.data.slice(0, 4).map((n) => (
                  <Meter key={n.id} label={n.name} value={n.allocated.memoryMb} max={n.capacity.memoryMb * n.overcommit} display={`${mb(n.allocated.memoryMb)} / ${mb(n.capacity.memoryMb * n.overcommit)}`} />
                ))}
                {nodes.data.length > 4 && (
                  <Link to="/nodes" className="inline-flex items-center gap-1 text-[13px] font-semibold text-brand-text">
                    All {nodes.data.length} nodes <ArrowRight className="size-3.5" />
                  </Link>
                )}
              </div>
            ) : (
              <p className="text-[13.5px] text-faint-foreground">No nodes yet.</p>
            )}
          </Card>
        </div>
      </div>

      <div className="grid gap-5 lg:grid-cols-3">
        <Card className="px-6 pt-5 pb-4 lg:col-span-2">
          <ChartHead title="CPU in use" value={fleet.length ? `${fleet[fleet.length - 1].cpuCores.toFixed(2)} cores` : undefined} hint="All bots, every minute" />
          <TimeChart label="CPU in use" height={200} points={fleet.map((p) => ({ t: p.t, v: p.cpuCores }))} format={(v) => (v === 0 ? '0' : `${v < 1 ? v.toFixed(2) : v.toFixed(1)} c`)} />
        </Card>
        <div className="flex flex-col gap-5">
          <Card className="px-6 pt-5 pb-5">
            <ChartHead title="Deploys" hint="Last 14 days" />
            <DayColumns
              height={150}
              days={days}
              series={[
                { label: 'Succeeded', color: 'var(--chart-1)' },
                { label: 'Failed', color: 'var(--chart-2)' },
              ]}
            />
          </Card>
          <PanelHealth nodes={d ? `${d.nodesOnline} of ${d.nodes} online` : '…'} nodesOk={d ? (d.nodes === 0 ? null : d.nodesOnline === d.nodes) : null} />
        </div>
      </div>
    </div>
  )
}

function ChartHead({ title, value, hint }: { title: string; value?: string; hint: string }) {
  return (
    <div className="mb-4 flex items-start justify-between gap-4">
      <div>
        <p className="font-display text-[17px] font-bold tracking-[-0.01em]">{title}</p>
        <p className="mt-0.5 text-[12.5px] text-faint-foreground">{hint}</p>
      </div>
      {value && <p className="font-display text-[22px] font-bold tracking-[-0.02em] tabular-nums">{value}</p>}
    </div>
  )
}

function PanelHealth({ nodes, nodesOk }: { nodes: string; nodesOk: boolean | null }) {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const t0 = performance.now()
      const res = await api.health()
      return { ...res, ms: Math.round(performance.now() - t0) }
    },
    refetchInterval: 15_000,
    retry: false,
  })
  const ok = health.data?.status === 'ok'
  const rows = [
    { label: 'API', ok: !health.isError, detail: health.data ? `${health.data.ms} ms` : health.isError ? 'unreachable' : '…' },
    { label: 'Database', ok, detail: health.isPending ? '…' : ok ? 'connected' : 'unreachable' },
    { label: 'Nodes', ok: nodesOk, detail: nodes },
  ]
  return (
    <Card className="flex-1 px-6 py-5">
      <div className="mb-2 flex items-center justify-between">
        <p className="font-display text-[17px] font-bold tracking-[-0.01em]">Panel</p>
        <span className="flex items-center gap-2 text-[12.5px] font-semibold text-muted-foreground">
          <StatusDot tone={health.isPending ? 'neutral' : ok ? 'success' : 'danger'} pulse={ok} />
          {health.isPending ? 'Checking' : ok ? 'Healthy' : 'Degraded'}
        </span>
      </div>
      <dl>
        {rows.map((r) => (
          <div key={r.label} className="flex items-center justify-between border-t py-2.5 text-[14px] first:border-t-0">
            <dt className="flex items-center gap-2.5 text-muted-foreground">
              <StatusDot tone={r.ok === null ? 'neutral' : r.ok ? 'success' : 'danger'} />
              {r.label}
            </dt>
            <dd className="font-mono text-[13px] text-foreground tabular-nums">{r.detail}</dd>
          </div>
        ))}
      </dl>
    </Card>
  )
}
