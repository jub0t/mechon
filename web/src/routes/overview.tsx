import { useQuery } from '@tanstack/react-query'
import { Bot, Check, ChevronDown, ChevronRight, Layers, Rocket, Server, ShieldCheck, Users } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { useMemo, useState } from 'react'
import { type DayColumn, DayColumns, StackedBar } from '@/components/data/bars'
import { Meter } from '@/components/data/meter'
import { Segmented } from '@/components/data/stat'
import { TimeChart } from '@/components/data/time-chart'
import { cn } from '@/lib/utils'
import { Card, CardHeader, StatusDot } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Reveal } from '@/components/motion/reveal'
import { Link } from 'react-router'
import { Stat } from '@/components/data/stat'
import { api, type User } from '@/lib/api'
import { bytes, mb } from '@/lib/format'

function greeting(d = new Date()) {
  const h = d.getHours()
  return h < 5 ? 'Up late' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
}

const today = () => new Date().toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })
const firstName = (name: string) => name.trim().split(/\s+/)[0] || name

export function OverviewPage({ user }: { user: User }) {
  return user.role === 'admin' ? <AdminOverview user={user} /> : <UserOverview user={user} />
}

// ---------- Admin ----------

type Step = { icon: typeof Server; title: string; text: string; done: boolean; href: string; cta: string }

function AdminOverview({ user }: { user: User }) {
  const o = useQuery({ queryKey: ['overview'], queryFn: api.overview, refetchInterval: 10000 })
  const d = o.data
  const steps: Step[] = [
    { icon: ShieldCheck, title: 'Create the admin account', text: 'That is you. Signed in and ready.', done: true, href: '/account', cta: 'Account' },
    { icon: Server, title: 'Add your first node', text: 'Install the agent on any server with Docker. It dials out to this panel, so no ports to open.', done: (d?.nodes ?? 0) > 0, href: '/nodes?new=1', cta: 'Add node' },
    { icon: Layers, title: 'Create a plan', text: 'Set the bots, memory, CPU and disk you sell. Limits are enforced by the kernel, not by trust.', done: (d?.plans ?? 0) > 0, href: '/plans?new=1', cta: 'New plan' },
    { icon: Users, title: 'Add a user', text: 'By hand, or automatically from your billing system through the API.', done: (d?.users ?? 0) > 0, href: '/users?new=1', cta: 'New user' },
    { icon: Rocket, title: 'Deploy a bot', text: 'From a discord.js, discord.py or Bun template, by upload or API.', done: (d?.bots ?? 0) > 0, href: '/bots?new=1', cta: 'New bot' },
  ]
  const done = steps.filter((s) => s.done).length
  const running = d?.botsByState.running ?? 0
  const crashed = d?.botsByState.crashed ?? 0

  return (
    <>
      <Reveal>
        <PageHeader
          eyebrow={today()}
          title={`${greeting()}, ${firstName(user.name)}.`}
          description={done < steps.length ? 'This is your hosting panel. Work through the setup below and you will have a bot running in its own sandbox.' : 'Everything your hosting runs on, at a glance.'}
        />
      </Reveal>

      <Reveal delay={0.04}>
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

      <div className="grid gap-5 lg:grid-cols-3">
        <Reveal delay={0.08} className="lg:col-span-2">
          <Checklist steps={steps} done={done} />
        </Reveal>

        <div className="flex flex-col gap-5">
          <Reveal delay={0.12}>
            <Card className="px-6 py-5">
              <div className="mb-4 flex items-baseline justify-between">
                <p className="font-display text-[17px] font-bold tracking-[-0.01em]">Bots by state</p>
                <span className="text-[13px] font-semibold text-muted-foreground tabular-nums">{d?.bots ?? 0} total</span>
              </div>
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
          </Reveal>
          <Reveal delay={0.16}>
            <PanelHealth nodes={d ? `${d.nodesOnline} of ${d.nodes} online` : '…'} nodesOk={d ? (d.nodes === 0 ? null : d.nodesOnline === d.nodes) : null} />
          </Reveal>
        </div>
      </div>

      <Reveal delay={0.2}>
        <Activity />
      </Reveal>
    </>
  )
}

function Checklist({ steps, done }: { steps: Step[]; done: number }) {
  const complete = done === steps.length
  const [open, setOpen] = useState(() => {
    try {
      const saved = localStorage.getItem('mechon-setup-open')
      if (saved != null) return saved === '1'
    } catch {
      // storage blocked: fall through to the default
    }
    return !complete
  })
  const toggle = () =>
    setOpen((o) => {
      try {
        localStorage.setItem('mechon-setup-open', o ? '0' : '1')
      } catch {
        // the toggle still works for this visit
      }
      return !o
    })
  const reduce = useReducedMotion()
  return (
    <Card className="overflow-hidden">
      <button
        type="button"
        onClick={toggle}
        aria-expanded={open}
        aria-controls="setup-steps"
        className="flex w-full items-center justify-between gap-4 px-6 pt-5 pb-3 text-left"
      >
        <h2 className="font-display text-[17px] font-bold tracking-[-0.01em]">{complete ? 'Your host is set up' : 'Get your host ready'}</h2>
        <span className="flex items-center gap-3">
          <span className="text-[13px] font-semibold text-muted-foreground tabular-nums">
            {done} of {steps.length}
          </span>
          <span className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground">
            <ChevronDown className={cn('size-[18px] transition-transform duration-200', open && 'rotate-180')} />
          </span>
        </span>
      </button>
      <div className={cn('px-6', open ? 'pb-2' : 'pb-5')}>
        <Progress value={done / steps.length} />
      </div>
      <AnimatePresence initial={false}>
        {open && (
          <motion.ol
            id="setup-steps"
            className="mt-2 overflow-hidden"
            initial={reduce ? false : { height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={reduce ? undefined : { height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
          >
            {steps.map((s, i) => (
              <StepRow key={s.title} step={s} index={i} />
            ))}
          </motion.ol>
        )}
      </AnimatePresence>
    </Card>
  )
}

function Activity() {
  const [range, setRange] = useState<'1h' | '24h' | '7d'>('24h')
  const a = useQuery({ queryKey: ['activity', range], queryFn: () => api.activity(range), refetchInterval: 60_000, placeholderData: (prev) => prev })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes, refetchInterval: 15_000 })
  const fleet = a.data?.fleet ?? []
  const days = useMemo(() => {
    const byDay = new Map((a.data?.deploys ?? []).map((d) => [new Date(d.day).toDateString(), d]))
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
  return (
    <div className="mt-8">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-title text-[22px]">Activity</h2>
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
      <div className={cn('grid gap-5 transition-opacity lg:grid-cols-3', a.isFetching && a.isPlaceholderData && 'opacity-60')}>
        <Card className="px-6 pt-5 pb-4 lg:col-span-2">
          <ChartHead title="Memory in use" value={fleet.length ? bytes(fleet[fleet.length - 1].memory) : undefined} hint="All bots, every minute" />
          <TimeChart label="Memory in use" bytes points={fleet.map((p) => ({ t: p.t, v: p.memory }))} format={bytesShort} />
        </Card>
        <Card className="px-6 pt-5 pb-5">
          <ChartHead title="Deploys" hint="Last 14 days" />
          <DayColumns
            days={days}
            series={[
              { label: 'Succeeded', color: 'var(--chart-1)' },
              { label: 'Failed', color: 'var(--chart-2)' },
            ]}
          />
        </Card>
        <Card className="px-6 pt-5 pb-4 lg:col-span-2">
          <ChartHead title="CPU in use" value={fleet.length ? `${fleet[fleet.length - 1].cpuCores.toFixed(2)} cores` : undefined} hint="All bots, every minute" />
          <TimeChart label="CPU in use" points={fleet.map((p) => ({ t: p.t, v: p.cpuCores }))} format={(v) => `${v < 1 ? v.toFixed(2) : v.toFixed(1)} c`} />
        </Card>
        <Card className="px-6 pt-5 pb-5">
          <ChartHead title="Capacity by node" hint="Memory sold of what each node sells" />
          {nodes.data?.length ? (
            <div className="space-y-4">
              {nodes.data.map((n) => (
                <Meter
                  key={n.id}
                  label={n.name}
                  value={n.allocated.memoryMb}
                  max={n.capacity.memoryMb * n.overcommit}
                  display={`${mb(n.allocated.memoryMb)} / ${mb(n.capacity.memoryMb * n.overcommit)}`}
                />
              ))}
            </div>
          ) : (
            <p className="text-[13.5px] text-faint-foreground">No nodes yet.</p>
          )}
        </Card>
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

const bytesShort = (v: number) => (v === 0 ? '0' : bytes(v).replace('.0 ', ' '))

function Progress({ value }: { value: number }) {
  const reduce = useReducedMotion()
  return (
    <div className="h-2 overflow-hidden rounded-full bg-surface-2" role="progressbar" aria-valuenow={Math.round(value * 100)} aria-valuemin={0} aria-valuemax={100}>
      <motion.div
        className="h-full rounded-full bg-brand"
        initial={reduce ? false : { width: 0 }}
        animate={{ width: `${Math.max(value * 100, 4)}%` }}
        transition={{ duration: 0.9, delay: 0.3, ease: [0.22, 1, 0.36, 1] }}
      />
    </div>
  )
}

function StepRow({ step, index }: { step: Step; index: number }) {
  return (
    <li className="border-t first:border-t-0">
      <Link to={step.href} className="group flex items-start gap-4 px-6 py-4 transition-colors hover:bg-surface-2/60 focus-visible:bg-surface-2/60 focus-visible:outline-none">
        <span
          className={cn(
            'mt-0.5 flex size-10 shrink-0 items-center justify-center rounded-[12px] transition-colors',
            step.done ? 'bg-brand text-brand-foreground' : 'bg-surface-2 text-muted-foreground group-hover:bg-brand-soft group-hover:text-brand-text',
          )}
        >
          {step.done ? <Check className="size-[18px]" strokeWidth={2.75} /> : <step.icon className="size-[18px]" strokeWidth={2.25} />}
        </span>
        <div className="min-w-0 flex-1">
          <p className={cn('text-[15px] font-semibold', step.done && 'text-muted-foreground line-through decoration-faint-foreground/60')}>
            <span className="mr-1.5 text-faint-foreground tabular-nums">{index + 1}.</span>
            {step.title}
          </p>
          <p className="mt-1 text-[14px] leading-relaxed text-muted-foreground">{step.text}</p>
        </div>
        {step.done ? (
          <span className="mt-1 inline-flex h-7 items-center rounded-full bg-brand-soft px-2.5 text-[12.5px] font-semibold text-brand-text">Done</span>
        ) : (
          <span className="mt-0.5 inline-flex h-9 shrink-0 items-center gap-1 rounded-full border-[1.5px] border-foreground/15 px-4 text-sm font-semibold transition-colors group-hover:border-foreground/40">
            {step.cta} <ChevronRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </span>
        )}
      </Link>
    </li>
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
    <Card>
      <CardHeader
        title="Panel"
        aside={
          <span className="flex items-center gap-2 text-[12.5px] font-semibold text-muted-foreground">
            <StatusDot tone={health.isPending ? 'neutral' : ok ? 'success' : 'danger'} pulse={ok} />
            {health.isPending ? 'Checking' : ok ? 'Healthy' : 'Degraded'}
          </span>
        }
      />
      <dl className="px-6 pb-5">
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

// ---------- User ----------

function UserOverview({ user }: { user: User }) {
  return (
    <>
      <Reveal>
        <PageHeader eyebrow={today()} title={`${greeting()}, ${firstName(user.name)}.`} description="Your bots live here, each in its own sandbox." />
      </Reveal>
      <Reveal delay={0.08}>
        <Card className="flex flex-col items-center px-6 py-20 text-center">
          <EmptySlats />
          <p className="mt-8 font-display text-[22px] font-bold tracking-[-0.015em]">No bots yet</p>
          <p className="mt-2 max-w-[44ch] text-[15px] leading-relaxed text-muted-foreground">
            Once your host gives you a plan, you can deploy a discord.js, discord.py or Bun bot here and watch it come online.
          </p>
          <span className="mt-6 inline-flex h-8 items-center gap-2 rounded-full bg-surface-2 px-3.5 text-[13px] font-semibold text-muted-foreground">
            <Bot className="size-4" strokeWidth={2.25} /> Deploys arrive in milestone 4
          </span>
        </Card>
      </Reveal>
    </>
  )
}

/** Three empty lanes, one breathing: a bot slot waiting to be filled. */
function EmptySlats() {
  const reduce = useReducedMotion()
  return (
    <div className="flex h-20 items-end gap-2.5" aria-hidden>
      {[0.55, 1, 0.75].map((h, i) => (
        <motion.span
          key={i}
          className={cn('w-5 rounded-full', i === 1 ? 'bg-brand' : 'bg-surface-2')}
          style={{ height: `${h * 100}%` }}
          animate={reduce || i !== 1 ? undefined : { opacity: [1, 0.45, 1] }}
          transition={{ duration: 2.2, repeat: Infinity, ease: 'easeInOut' }}
        />
      ))}
    </div>
  )
}
