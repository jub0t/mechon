import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot as BotIcon, Check, ChevronRight, Plus } from 'lucide-react'
import { type FormEvent, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { toast } from 'sonner'
import { Field, FormError } from '@/components/data/field'
import { MiniBar } from '@/components/data/meter'
import { BotStateBadge } from '@/components/data/state-badge'
import { EmptyState } from '@/components/data/stat'
import { Reveal } from '@/components/motion/reveal'
import { Card } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Bot, type Subscription, type Template } from '@/lib/api'
import { useMe } from '@/lib/auth'
import { bytes, cores, mb, pct } from '@/lib/format'
import { cn } from '@/lib/utils'

export const templateTint: Record<string, string> = {
  'discord-js': 'bg-[oklch(0.62_0.16_145/0.16)] text-[oklch(0.78_0.14_145)]',
  'discord-py': 'bg-[oklch(0.65_0.13_245/0.18)] text-[oklch(0.8_0.1_245)]',
  bun: 'bg-orange-soft text-orange-text',
}

export function TemplateMark({ id, className }: { id: string; className?: string }) {
  const label = { 'discord-js': 'JS', 'discord-py': 'PY', bun: 'BUN' }[id] ?? id.slice(0, 2).toUpperCase()
  return (
    <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-[12px] font-mono text-[12px] font-bold', templateTint[id] ?? 'bg-surface-2 text-muted-foreground', className)}>
      {label}
    </span>
  )
}

/** The bot list. Admins see every bot with its owner; users see their own. */
export function BotsPage({ title = 'Bots', description }: { title?: string; description?: string }) {
  const me = useMe().data!
  const bots = useQuery({ queryKey: ['bots'], queryFn: () => api.bots(), refetchInterval: 5000 })
  const [creating, setCreating] = useState(false)
  const admin = me.role === 'admin'

  return (
    <>
      <Reveal>
        <PageHeader
          title={title}
          description={description ?? (admin ? 'Every bot on every node, with its owner, state and resource use.' : 'Each bot runs in its own sandbox.')}
          actions={
            <Button onClick={() => setCreating(true)}>
              <Plus strokeWidth={2.25} /> New bot
            </Button>
          }
        />
      </Reveal>
      <BotList bots={bots.data} loading={bots.isPending} showOwner={admin} onCreate={() => setCreating(true)} />
      <NewBotDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}

export function BotList({ bots, loading, showOwner, onCreate }: { bots?: Bot[]; loading: boolean; showOwner: boolean; onCreate: () => void }) {
  if (loading)
    return (
      <div className="space-y-3">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-[76px] rounded-[18px]" />
        ))}
      </div>
    )
  if (!bots?.length)
    return (
      <Reveal delay={0.05}>
        <Card>
          <EmptyState
            icon={<BotIcon className="size-6" strokeWidth={2.25} />}
            title="No bots yet"
            text="Pick a template, give it a token and upload your code. It comes online in its own sandbox."
            action={
              <Button onClick={onCreate}>
                <Plus strokeWidth={2.25} /> New bot
              </Button>
            }
          />
        </Card>
      </Reveal>
    )
  return (
    <div className="space-y-2.5">
      {bots.map((b, i) => (
        <Reveal key={b.id} delay={Math.min(i * 0.03, 0.3)}>
          <Link
            to={`/bots/${b.id}`}
            className="group grid grid-cols-[auto_1fr_auto] items-center gap-4 rounded-[18px] border bg-surface px-4 py-3.5 transition-colors hover:border-foreground/20 sm:grid-cols-[auto_minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_auto] sm:px-5"
          >
            <TemplateMark id={b.template} />
            <div className="min-w-0">
              <p className="truncate text-[15px] font-semibold">{b.name}</p>
              <p className="truncate text-[13px] text-muted-foreground">{showOwner ? `${b.owner.name} · ${b.node.name}` : `${b.plan} · ${mb(b.limits.memoryMb)}`}</p>
            </div>
            <div className="hidden min-w-0 sm:block">
              <p className="mb-1.5 flex justify-between text-[12px] text-faint-foreground">
                <span>Memory</span>
                <span className="font-mono tabular-nums">{b.usage ? bytes(b.usage.memoryBytes) : '—'}</span>
              </p>
              <MiniBar value={b.usage?.memoryBytes ?? 0} max={b.limits.memoryMb * 2 ** 20} />
            </div>
            <div className="hidden min-w-0 sm:block">
              <p className="mb-1.5 flex justify-between text-[12px] text-faint-foreground">
                <span>CPU</span>
                <span className="font-mono tabular-nums">{b.usage ? pct(b.usage.cpuPercent) : '—'}</span>
              </p>
              <MiniBar value={b.usage?.cpuPercent ?? 0} max={100} />
            </div>
            <div className="flex items-center gap-2">
              <BotStateBadge state={b.state} />
              <ChevronRight className="size-4 text-faint-foreground transition-transform group-hover:translate-x-0.5" />
            </div>
          </Link>
        </Reveal>
      ))}
    </div>
  )
}

// ---------- New bot ----------

const sizes = [
  { id: 's', label: 'Small', memoryMb: 256, cpuMillicores: 250, diskMb: 1024 },
  { id: 'm', label: 'Medium', memoryMb: 512, cpuMillicores: 500, diskMb: 2048 },
  { id: 'l', label: 'Large', memoryMb: 1024, cpuMillicores: 1000, diskMb: 4096 },
]

function room(sub: Subscription) {
  return {
    bots: sub.plan.maxBots - sub.used.bots,
    memoryMb: sub.plan.memoryMb - sub.used.memoryMb,
    cpuMillicores: sub.plan.cpuMillicores - sub.used.cpuMillicores,
    diskMb: sub.plan.diskMb - sub.used.diskMb,
  }
}

export function NewBotDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92svh] overflow-y-auto sm:max-w-[620px]">{open && <NewBotForm onDone={() => onOpenChange(false)} />}</DialogContent>
    </Dialog>
  )
}

function NewBotForm({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const templates = useQuery({ queryKey: ['templates'], queryFn: api.templates })
  const subs = useQuery({ queryKey: ['my-subs'], queryFn: api.mySubscriptions })
  const active = useMemo(() => subs.data?.filter((s) => s.status === 'active') ?? [], [subs.data])
  const [subId, setSubId] = useState<string>('')
  const [template, setTemplate] = useState('discord-js')
  const [name, setName] = useState('')
  const [size, setSize] = useState<{ memoryMb: number; cpuMillicores: number; diskMb: number }>(sizes[1])
  const [env, setEnv] = useState<Record<string, string>>({})

  const sub = active.find((s) => s.id === subId) ?? active[0]
  const left = sub ? room(sub) : null
  const tpl: Template | undefined = templates.data?.find((t) => t.id === template)
  const fits = (s: typeof size) => !!left && left.bots > 0 && s.memoryMb <= left.memoryMb && s.cpuMillicores <= left.cpuMillicores && s.diskMb <= left.diskMb
  const allowed = (t: Template) => !sub || sub.plan.templates.length === 0 || sub.plan.templates.includes(t.id)

  const create = useMutation({
    mutationFn: () => api.createBot({ subscriptionId: sub!.id, name, template, env, ...size }),
    onSuccess: (bot) => {
      qc.invalidateQueries({ queryKey: ['bots'] })
      qc.invalidateQueries({ queryKey: ['my-subs'] })
      toast.success(`${bot.name} is ready for its first deploy`)
      onDone()
      navigate(`/bots/${bot.id}?tab=deploys`)
    },
  })

  if (subs.isPending || templates.isPending) return <Skeleton className="h-96 rounded-[20px]" />
  if (!active.length)
    return (
      <EmptyState
        icon={<BotIcon className="size-6" />}
        title="You do not have a plan yet"
        text="Bots run inside a plan that sets their memory, CPU and disk. Ask your host to give you one."
      />
    )

  return (
    <form
      className="space-y-6"
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        create.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[24px] font-bold tracking-[-0.02em]">New bot</DialogTitle>
        <DialogDescription>Pick a template and a size. You upload the code on the next screen.</DialogDescription>
      </DialogHeader>

      <div className="grid gap-2.5 sm:grid-cols-3">
        {templates.data!.map((t) => {
          const on = t.id === template
          const ok = allowed(t)
          return (
            <button
              key={t.id}
              type="button"
              disabled={!ok}
              onClick={() => setTemplate(t.id)}
              className={cn(
                'relative flex flex-col items-start gap-3 rounded-[16px] border p-4 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40',
                on ? 'border-brand bg-brand-soft/60 ring-4 ring-brand-soft' : 'hover:border-foreground/25',
              )}
            >
              <TemplateMark id={t.id} />
              <span>
                <span className="block text-[15px] font-semibold">{t.name}</span>
                <span className="block text-[12.5px] text-muted-foreground">{t.language}</span>
              </span>
              {on && (
                <span className="absolute top-3 right-3 flex size-5 items-center justify-center rounded-full bg-brand text-white">
                  <Check className="size-3" strokeWidth={3} />
                </span>
              )}
            </button>
          )
        })}
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" htmlFor="b-name">
          <Input id="b-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="ticket-tool" autoFocus />
        </Field>
        <Field label="Plan" htmlFor="b-plan">
          <Select value={sub?.id} onValueChange={setSubId}>
            <SelectTrigger id="b-plan">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {active.map((s) => (
                <SelectItem key={s.id} value={s.id}>
                  {s.plan.name} · {s.plan.maxBots - s.used.bots} of {s.plan.maxBots} bots free
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>

      <div>
        <div className="mb-2 flex items-baseline justify-between">
          <p className="text-[13.5px] font-semibold">Size</p>
          {left && (
            <p className="text-[12.5px] text-faint-foreground tabular-nums">
              Left in plan: {mb(Math.max(left.memoryMb, 0))} · {cores(Math.max(left.cpuMillicores, 0))} · {mb(Math.max(left.diskMb, 0))}
            </p>
          )}
        </div>
        <div className="grid gap-2.5 sm:grid-cols-3">
          {sizes.map((s) => {
            const on = s.memoryMb === size.memoryMb && s.cpuMillicores === size.cpuMillicores && s.diskMb === size.diskMb
            const ok = fits(s)
            return (
              <button
                key={s.id}
                type="button"
                disabled={!ok}
                onClick={() => setSize(s)}
                className={cn(
                  'rounded-[16px] border px-4 py-3.5 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40',
                  on ? 'border-brand bg-brand-soft/60 ring-4 ring-brand-soft' : 'hover:border-foreground/25',
                )}
              >
                <span className="block text-[15px] font-semibold">{s.label}</span>
                <span className="mt-0.5 block font-mono text-[12px] text-muted-foreground">
                  {mb(s.memoryMb)} · {cores(s.cpuMillicores)} · {mb(s.diskMb)}
                </span>
              </button>
            )
          })}
        </div>
        {left && left.bots <= 0 && <p className="mt-2 text-[13px] text-orange-text">This plan has no bot slots left.</p>}
      </div>

      {tpl?.env.map((v) => (
        <Field key={v.key} label={v.label} htmlFor={`env-${v.key}`} hint={v.secret ? 'Encrypted at rest. Nobody can read it back, not even admins.' : `Default ${v.default}`}>
          <Input
            id={`env-${v.key}`}
            type={v.secret ? 'password' : 'text'}
            autoComplete="off"
            value={env[v.key] ?? ''}
            placeholder={v.default}
            onChange={(e) => setEnv((s) => ({ ...s, [v.key]: e.target.value }))}
            className="font-mono text-[14px]"
          />
        </Field>
      ))}

      <FormError error={create.error} />
      <DialogFooter className="gap-2">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" variant="brand" disabled={create.isPending || !name || !fits(size)}>
          {create.isPending ? 'Creating…' : 'Create bot'}
        </Button>
      </DialogFooter>
    </form>
  )
}
