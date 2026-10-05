import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, Trash2 } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { toast } from 'sonner'
import { Field, FormError } from '@/components/data/field'
import { Pill } from '@/components/data/state-badge'
import { Reveal } from '@/components/motion/reveal'
import { Card, CardHeader } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { api, type User } from '@/lib/api'
import { meKey, useMe } from '@/lib/auth'
import { ago } from '@/lib/format'
import { cn } from '@/lib/utils'
import { CopyBlock } from './nodes'

export function AccountPage() {
  const me = useMe().data!
  return (
    <>
      <Reveal>
        <PageHeader title="Account" description="Your profile, password and API keys." />
      </Reveal>
      <div className="grid gap-5 lg:grid-cols-2">
        <Reveal delay={0.04}>
          <Profile me={me} />
        </Reveal>
        <Reveal delay={0.08}>
          <Password />
        </Reveal>
        <Reveal delay={0.12} className="lg:col-span-2">
          <Keys me={me} />
        </Reveal>
      </div>
    </>
  )
}

function Profile({ me }: { me: User }) {
  const qc = useQueryClient()
  const [name, setName] = useState(me.name)
  const [email, setEmail] = useState(me.email)
  const save = useMutation({
    mutationFn: () => api.updateMe({ name, email }),
    onSuccess: (u) => {
      qc.setQueryData(meKey, u)
      toast.success('Saved')
    },
  })
  return (
    <Card className="h-full px-6 py-5">
      <form
        className="space-y-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <p className="font-display text-[17px] font-bold">Profile</p>
        <Field label="Name" htmlFor="a-name">
          <Input id="a-name" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Email" htmlFor="a-email">
          <Input id="a-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <FormError error={save.error} />
        <Button type="submit" disabled={save.isPending || (name === me.name && email === me.email)}>
          Save
        </Button>
      </form>
    </Card>
  )
}

function Password() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const save = useMutation({
    mutationFn: () => api.changePassword(current, next),
    onSuccess: () => {
      setCurrent('')
      setNext('')
      toast.success('Password changed. Other sessions were signed out.')
    },
  })
  return (
    <Card className="h-full px-6 py-5">
      <form
        className="space-y-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <p className="font-display text-[17px] font-bold">Password</p>
        <Field label="Current password" htmlFor="a-cur">
          <Input id="a-cur" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </Field>
        <Field label="New password" htmlFor="a-new" hint="At least 10 characters.">
          <Input id="a-new" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <FormError error={save.error} />
        <Button type="submit" disabled={save.isPending || !current || next.length < 10}>
          Change password
        </Button>
      </form>
    </Card>
  )
}

const scopeInfo: Record<string, string> = {
  'bots:read': 'Read bots, logs and metrics',
  'bots:write': 'Create, change and deploy bots',
  operator: 'Admin API: users, plans, nodes',
}

function Keys({ me }: { me: User }) {
  const qc = useQueryClient()
  const keys = useQuery({ queryKey: ['keys'], queryFn: api.keys })
  const [creating, setCreating] = useState(false)
  const [secret, setSecret] = useState<string | null>(null)
  const revoke = useMutation({
    mutationFn: (id: string) => api.deleteKey(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['keys'] }),
  })
  return (
    <Card className="overflow-hidden">
      <CardHeader
        title="API keys"
        aside={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-4" /> New key
          </Button>
        }
      />
      <p className="px-6 pb-4 text-[14px] text-muted-foreground">
        For CI deploys and billing integrations. Send it as <code className="font-mono text-[13px]">Authorization: Bearer mk_…</code>
      </p>
      {keys.data?.length === 0 && <p className="border-t px-6 py-8 text-center text-[14px] text-faint-foreground">No keys yet.</p>}
      <ul>
        {keys.data?.map((k) => (
          <li key={k.id} className="flex flex-wrap items-center gap-3 border-t px-6 py-3.5">
            <span className="flex size-9 items-center justify-center rounded-[11px] bg-surface-2 text-muted-foreground">
              <KeyRound className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="font-semibold">{k.name}</p>
              <p className="text-[12.5px] text-muted-foreground">
                <span className="font-mono">{k.prefix}…</span> · created {ago(k.createdAt)} · last used {ago(k.lastUsedAt)}
              </p>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {k.scopes.map((s) => (
                <Pill key={s} tone={s === 'operator' ? 'brand' : 'neutral'}>
                  {s}
                </Pill>
              ))}
            </div>
            <Button variant="ghost" size="icon" onClick={() => revoke.mutate(k.id)} aria-label={`Revoke ${k.name}`}>
              <Trash2 className="size-4" />
            </Button>
          </li>
        ))}
      </ul>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-[500px]">
          {creating && (
            <NewKey
              admin={me.role === 'admin'}
              onCreated={(s) => {
                setCreating(false)
                setSecret(s)
                qc.invalidateQueries({ queryKey: ['keys'] })
              }}
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={!!secret} onOpenChange={(o) => !o && setSecret(null)}>
        <DialogContent className="sm:max-w-[560px]">
          <DialogHeader>
            <DialogTitle className="font-display text-[22px] font-bold">Copy your key now</DialogTitle>
            <DialogDescription>This is the only time it is shown. Store it somewhere safe.</DialogDescription>
          </DialogHeader>
          {secret && <CopyBlock label="API key" text={secret} />}
        </DialogContent>
      </Dialog>
    </Card>
  )
}

function NewKey({ admin, onCreated }: { admin: boolean; onCreated: (secret: string) => void }) {
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<string[]>(['bots:read', 'bots:write'])
  const create = useMutation({ mutationFn: () => api.createKey({ name, scopes }), onSuccess: (k) => onCreated(k.secret) })
  const all = admin ? ['bots:read', 'bots:write', 'operator'] : ['bots:read', 'bots:write']
  return (
    <form
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault()
        create.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold">New API key</DialogTitle>
      </DialogHeader>
      <Field label="Name" htmlFor="k-name" hint="What uses it, e.g. GitHub Actions.">
        <Input id="k-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </Field>
      <div className="space-y-2">
        {all.map((s) => {
          const on = scopes.includes(s)
          return (
            <button
              key={s}
              type="button"
              onClick={() => setScopes((cur) => (on ? cur.filter((x) => x !== s) : [...cur, s]))}
              className={cn('flex w-full items-center justify-between rounded-[14px] border px-4 py-3 text-left transition-colors', on ? 'border-brand bg-brand-soft/60' : 'hover:border-foreground/25')}
            >
              <span>
                <span className="block font-mono text-[13.5px] font-semibold">{s}</span>
                <span className="block text-[12.5px] text-muted-foreground">{scopeInfo[s]}</span>
              </span>
              <span className={cn('size-5 rounded-full border-2', on ? 'border-brand bg-brand' : 'border-foreground/25')} />
            </button>
          )
        })}
      </div>
      <FormError error={create.error} />
      <DialogFooter>
        <Button type="submit" disabled={create.isPending || !name || scopes.length === 0}>
          Create key
        </Button>
      </DialogFooter>
    </form>
  )
}
