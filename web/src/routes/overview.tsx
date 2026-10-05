import { useQuery } from '@tanstack/react-query'
import { Bot, Check, ChevronRight, Layers, Rocket, Server, ShieldCheck, Users } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { Card, CardHeader, StatusDot } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Reveal } from '@/components/motion/reveal'
import { isMac, Kbd } from '@/components/shell/command-palette'
import { Link } from 'react-router'
import { Stat } from '@/components/data/stat'
import { api, type User } from '@/lib/api'
import { mb } from '@/lib/format'
import { cn } from '@/lib/utils'

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
          <Stat label="Customers" value={d?.users ?? '…'} hint={`${d?.plans ?? 0} plans on sale`} />
          <Stat
            label="Memory sold"
            value={d ? (d.capacity.memoryMb ? `${Math.round((d.allocated.memoryMb / d.capacity.memoryMb) * 100)}%` : '0%') : '…'}
            hint={d ? `${mb(d.allocated.memoryMb)} of ${mb(d.capacity.memoryMb)}` : ''}
          />
        </div>
      </Reveal>

      <div className="grid gap-5 lg:grid-cols-3">
        <Reveal delay={0.08} className="lg:col-span-2">
          <Card className="overflow-hidden">
            <CardHeader
              title={done === steps.length ? 'Your host is set up' : 'Get your host ready'}
              aside={
                <span className="text-[13px] font-semibold text-muted-foreground tabular-nums">
                  {done} of {steps.length}
                </span>
              }
            />
            <div className="px-6 pb-2">
              <Progress value={done / steps.length} />
            </div>
            <ol className="mt-2">
              {steps.map((s, i) => (
                <StepRow key={s.title} step={s} index={i} />
              ))}
            </ol>
          </Card>
        </Reveal>

        <div className="flex flex-col gap-5">
          <Reveal delay={0.12}>
            <PanelHealth nodes={d ? `${d.nodesOnline} of ${d.nodes} online` : '…'} nodesOk={d ? d.nodes === 0 ? null : d.nodesOnline === d.nodes : null} />
          </Reveal>
          <Reveal delay={0.18}>
            <Card className="px-6 py-5">
              <p className="font-display text-[17px] font-bold tracking-[-0.01em]">Move faster</p>
              <p className="mt-2 text-[14.5px] leading-relaxed text-muted-foreground">
                Press <Kbd>{isMac ? '⌘' : 'Ctrl'}</Kbd> <Kbd>K</Kbd> anywhere to jump between pages, switch theme or sign out.
              </p>
            </Card>
          </Reveal>
        </div>
      </div>
    </>
  )
}

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
