import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cpu, HardDrive, Layers, MemoryStick, Pencil, Plus, ShieldCheck, Bot as BotIcon } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { toast } from 'sonner'
import { Field, FormError } from '@/components/data/field'
import { EmptyState } from '@/components/data/stat'
import { Reveal } from '@/components/motion/reveal'
import { Card } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type Plan, type PlanInput } from '@/lib/api'
import { cores, mb } from '@/lib/format'

export function PlansPage() {
  const plans = useQuery({ queryKey: ['plans'], queryFn: api.plans })
  const [editing, setEditing] = useState<Plan | 'new' | null>(null)

  return (
    <>
      <Reveal>
        <PageHeader
          title="Plans"
          description="What you sell. A plan is a pool of bots, memory, CPU and disk that a user splits across their bots. The kernel enforces every limit."
          actions={
            <Button onClick={() => setEditing('new')}>
              <Plus strokeWidth={2.25} /> New plan
            </Button>
          }
        />
      </Reveal>

      {plans.isPending ? (
        <div className="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-64 rounded-[20px]" />
          ))}
        </div>
      ) : plans.data?.length ? (
        <div className="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
          {plans.data.map((p, i) => (
            <Reveal key={p.id} delay={0.04 * i}>
              <PlanCard plan={p} onEdit={() => setEditing(p)} />
            </Reveal>
          ))}
        </div>
      ) : (
        <Card>
          <EmptyState
            icon={<Layers className="size-6" strokeWidth={2.25} />}
            title="No plans yet"
            text="Create your first plan, for example 512 MB of memory and half a core for one bot. Then give it to users."
            action={
              <Button onClick={() => setEditing('new')}>
                <Plus strokeWidth={2.25} /> New plan
              </Button>
            }
          />
        </Card>
      )}

      <PlanDialog plan={editing} onClose={() => setEditing(null)} />
    </>
  )
}

function PlanCard({ plan, onEdit }: { plan: Plan; onEdit: () => void }) {
  const rows = [
    { icon: BotIcon, label: 'Bots', value: String(plan.maxBots) },
    { icon: MemoryStick, label: 'Memory', value: mb(plan.memoryMb) },
    { icon: Cpu, label: 'CPU', value: cores(plan.cpuMillicores) },
    { icon: HardDrive, label: 'Disk', value: mb(plan.diskMb) },
  ]
  return (
    <Card className="flex h-full flex-col px-6 py-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="font-display text-[21px] font-bold tracking-[-0.015em]">{plan.name}</p>
          <p className="mt-1 font-mono text-[12.5px] text-faint-foreground">{plan.slug}</p>
        </div>
        <Button variant="ghost" size="icon" onClick={onEdit} aria-label={`Edit ${plan.name}`}>
          <Pencil className="size-4" />
        </Button>
      </div>
      <dl className="mt-5 grid grid-cols-2 gap-3">
        {rows.map((r) => (
          <div key={r.label} className="rounded-[14px] bg-surface-2 px-3.5 py-3">
            <dt className="flex items-center gap-1.5 text-[12px] font-semibold text-muted-foreground">
              <r.icon className="size-3.5" strokeWidth={2.25} /> {r.label}
            </dt>
            <dd className="mt-1 font-display text-[19px] font-bold tabular-nums">{r.value}</dd>
          </div>
        ))}
      </dl>
      <div className="mt-auto flex flex-wrap items-center gap-2 pt-5 text-[13px] text-muted-foreground">
        <span className="font-semibold text-foreground tabular-nums">{plan.subscriptionCount}</span>
        {plan.subscriptionCount === 1 ? 'subscriber' : 'subscribers'}
        {plan.hardened && (
          <span className="ml-auto inline-flex h-7 items-center gap-1.5 rounded-full bg-brand-soft px-2.5 text-[12.5px] font-semibold text-brand-text">
            <ShieldCheck className="size-3.5" strokeWidth={2.5} /> gVisor
          </span>
        )}
      </div>
    </Card>
  )
}

const blank: PlanInput = { slug: '', name: '', maxBots: 1, memoryMb: 512, cpuMillicores: 500, diskMb: 2048, pidsMax: 128, hardened: false, templates: [] }

function PlanDialog({ plan, onClose }: { plan: Plan | 'new' | null; onClose: () => void }) {
  const open = plan !== null
  const editing = plan && plan !== 'new' ? plan : null
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-[560px]">
        {open && <PlanForm key={editing?.id ?? 'new'} initial={editing ?? blank} id={editing?.id} onDone={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function PlanForm({ initial, id, onDone }: { initial: PlanInput; id?: string; onDone: () => void }) {
  const qc = useQueryClient()
  const [v, setV] = useState<PlanInput>({ ...initial })
  const set = <K extends keyof PlanInput>(k: K, val: PlanInput[K]) => setV((s) => ({ ...s, [k]: val }))
  const save = useMutation({
    mutationFn: () => (id ? api.updatePlan(id, v) : api.createPlan(v)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['plans'] })
      toast.success(id ? 'Plan saved' : 'Plan created')
      onDone()
    },
  })
  const archive = useMutation({
    mutationFn: () => api.archivePlan(id!),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['plans'] })
      toast('Plan archived. Existing subscribers keep it.')
      onDone()
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate()
  }

  const num = (k: 'maxBots' | 'memoryMb' | 'diskMb' | 'pidsMax') => (e: React.ChangeEvent<HTMLInputElement>) => set(k, Number(e.target.value))

  return (
    <form onSubmit={submit} className="space-y-5">
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold tracking-[-0.015em]">{id ? `Edit ${initial.name}` : 'New plan'}</DialogTitle>
        <DialogDescription>Limits are a pool shared by all of a subscriber's bots.</DialogDescription>
      </DialogHeader>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" htmlFor="plan-name">
          <Input
            id="plan-name"
            value={v.name}
            autoFocus
            onChange={(e) => {
              set('name', e.target.value)
              if (!id) set('slug', e.target.value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, ''))
            }}
            placeholder="Starter"
          />
        </Field>
        <Field label="Slug" htmlFor="plan-slug" hint={id ? 'Fixed once created.' : 'Billing systems refer to the plan by this.'}>
          <Input id="plan-slug" value={v.slug} disabled={!!id} onChange={(e) => set('slug', e.target.value)} className="font-mono text-[14px]" />
        </Field>
        <Field label="Bots" htmlFor="plan-bots">
          <Input id="plan-bots" type="number" min={1} value={v.maxBots} onChange={num('maxBots')} />
        </Field>
        <Field label="Memory (MB)" htmlFor="plan-mem">
          <Input id="plan-mem" type="number" min={64} step={64} value={v.memoryMb} onChange={num('memoryMb')} />
        </Field>
        <Field label="CPU (cores)" htmlFor="plan-cpu">
          <Input
            id="plan-cpu"
            type="number"
            min={0.05}
            step={0.05}
            value={v.cpuMillicores / 1000}
            onChange={(e) => set('cpuMillicores', Math.round(Number(e.target.value) * 1000))}
          />
        </Field>
        <Field label="Disk (MB)" htmlFor="plan-disk">
          <Input id="plan-disk" type="number" min={128} step={128} value={v.diskMb} onChange={num('diskMb')} />
        </Field>
        <Field label="Process limit" htmlFor="plan-pids" hint="Per bot. Stops fork bombs.">
          <Input id="plan-pids" type="number" min={16} max={4096} value={v.pidsMax} onChange={num('pidsMax')} />
        </Field>
        <div className="flex items-start justify-between gap-4 rounded-[14px] bg-surface-2 px-4 py-3.5">
          <div>
            <p className="text-[14px] font-semibold">Hardened (gVisor)</p>
            <p className="mt-0.5 text-[12.5px] text-muted-foreground">An extra kernel boundary. Needs runsc on the node.</p>
          </div>
          <Switch checked={v.hardened} onCheckedChange={(c) => set('hardened', c)} aria-label="Hardened" />
        </div>
      </div>

      <FormError error={save.error ?? archive.error} />

      <DialogFooter className="gap-2 sm:justify-between">
        {id ? (
          <Button type="button" variant="ghost" className="text-danger hover:text-danger" onClick={() => archive.mutate()} disabled={archive.isPending}>
            Archive plan
          </Button>
        ) : (
          <span />
        )}
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" disabled={save.isPending || !v.name}>
            {id ? 'Save' : 'Create plan'}
          </Button>
        </div>
      </DialogFooter>
    </form>
  )
}
