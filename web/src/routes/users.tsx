import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Ban, KeyRound, Layers, MoreHorizontal, Plus, ShieldCheck, SlidersHorizontal, Trash2, UserRound, Users as UsersIcon } from 'lucide-react'
import { type FormEvent, useCallback, useState } from 'react'
import { useNewParam } from '@/hooks/use-new-param'
import { toast } from 'sonner'
import { Field, FormError } from '@/components/data/field'
import { Pill } from '@/components/data/state-badge'
import { EmptyState } from '@/components/data/stat'
import { Reveal } from '@/components/motion/reveal'
import { Card } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { initials } from '@/components/shell/user-menu'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { type AdminUser, api, type Limits, type Role, type Subscription } from '@/lib/api'
import { useMe } from '@/lib/auth'
import { ago, cores, mb } from '@/lib/format'
import { cn } from '@/lib/utils'

type Modal = { kind: 'create' } | { kind: 'password' | 'plans' | 'delete' | 'edit'; user: AdminUser } | null

export function UsersPage() {
  const users = useQuery({ queryKey: ['users'], queryFn: api.users })
  const me = useMe().data
  const qc = useQueryClient()
  const [modal, setModal] = useState<Modal>(null)
  useNewParam(useCallback(() => setModal({ kind: 'create' }), []))
  const suspend = useMutation({
    mutationFn: ({ id, s }: { id: string; s: boolean }) => api.suspendUser(id, s),
    onSuccess: (u) => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast(u.suspendedAt ? `${u.name} is suspended and their bots are stopping.` : `${u.name} can sign in again.`)
    },
    onError: (e) => toast.error(e.message),
  })

  return (
    <>
      <Reveal>
        <PageHeader
          title="Users"
          description="Your customers and fellow admins. Give users a plan and they can deploy bots within it."
          actions={
            <Button onClick={() => setModal({ kind: 'create' })}>
              <Plus strokeWidth={2.25} /> New user
            </Button>
          }
        />
      </Reveal>

      <Reveal delay={0.05}>
        <Card className="overflow-hidden">
          {users.isPending ? (
            <div className="space-y-3 p-6">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-12 rounded-[12px]" />
              ))}
            </div>
          ) : !users.data?.length ? (
            <EmptyState icon={<UsersIcon className="size-6" />} title="No users yet" />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-[14px]">
                <thead>
                  <tr className="text-left text-[12px] font-semibold tracking-[0.04em] text-faint-foreground uppercase">
                    <th className="border-b py-3 pr-4 pl-6">User</th>
                    <th className="border-b px-4 py-3">Role</th>
                    <th className="border-b px-4 py-3">Plans</th>
                    <th className="border-b px-4 py-3">Bots</th>
                    <th className="border-b px-4 py-3">Joined</th>
                    <th className="border-b py-3 pr-6 pl-4" />
                  </tr>
                </thead>
                <tbody>
                  {users.data.map((u) => (
                    <tr key={u.id} className={cn('transition-colors hover:bg-surface-2/50 [&:last-child>td]:border-b-0', u.suspendedAt && 'opacity-60')}>
                      <td className="border-b py-3 pr-4 pl-6">
                        <div className="flex items-center gap-3">
                          <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-brand-soft font-display text-[13px] font-bold text-brand-text">
                            {initials(u.name)}
                          </span>
                          <div className="min-w-0">
                            <p className="truncate font-semibold">
                              {u.name}
                              {u.id === me?.id && <span className="ml-2 text-[12px] font-medium text-faint-foreground">you</span>}
                            </p>
                            <p className="truncate text-[13px] text-muted-foreground">{u.email}</p>
                          </div>
                        </div>
                      </td>
                      <td className="border-b px-4 py-3">
                        {u.suspendedAt ? (
                          <Pill tone="danger">Suspended</Pill>
                        ) : u.role === 'admin' ? (
                          <Pill tone="brand">Admin</Pill>
                        ) : (
                          <Pill tone="neutral">User</Pill>
                        )}
                      </td>
                      <td className="border-b px-4 py-3 tabular-nums">{u.subscriptionCount}</td>
                      <td className="border-b px-4 py-3 tabular-nums">{u.botCount}</td>
                      <td className="border-b px-4 py-3 text-muted-foreground">{ago(u.createdAt)}</td>
                      <td className="border-b py-3 pr-6 pl-4 text-right">
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" aria-label={`Actions for ${u.name}`}>
                              <MoreHorizontal className="size-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="min-w-52 rounded-[14px] p-1.5">
                            <DropdownMenuItem className="rounded-[10px]" onSelect={() => setModal({ kind: 'plans', user: u })}>
                              <Layers /> Plans
                            </DropdownMenuItem>
                            <DropdownMenuItem className="rounded-[10px]" onSelect={() => setModal({ kind: 'edit', user: u })}>
                              <UserRound /> Edit details
                            </DropdownMenuItem>
                            <DropdownMenuItem className="rounded-[10px]" onSelect={() => setModal({ kind: 'password', user: u })}>
                              <KeyRound /> Set password
                            </DropdownMenuItem>
                            {u.id !== me?.id && (
                              <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem className="rounded-[10px]" onSelect={() => suspend.mutate({ id: u.id, s: !u.suspendedAt })}>
                                  {u.suspendedAt ? <ShieldCheck /> : <Ban />} {u.suspendedAt ? 'Unsuspend' : 'Suspend'}
                                </DropdownMenuItem>
                                <DropdownMenuItem className="rounded-[10px]" variant="destructive" onSelect={() => setModal({ kind: 'delete', user: u })}>
                                  <Trash2 /> Delete
                                </DropdownMenuItem>
                              </>
                            )}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </Reveal>

      <Dialog open={modal?.kind === 'create' || modal?.kind === 'edit'} onOpenChange={(o) => !o && setModal(null)}>
        <DialogContent className="sm:max-w-[520px]">
          {modal?.kind === 'create' && <UserForm onDone={() => setModal(null)} />}
          {modal?.kind === 'edit' && <UserForm user={modal.user} onDone={() => setModal(null)} />}
        </DialogContent>
      </Dialog>
      <Dialog open={modal?.kind === 'password'} onOpenChange={(o) => !o && setModal(null)}>
        <DialogContent className="sm:max-w-[440px]">{modal?.kind === 'password' && <PasswordForm user={modal.user} onDone={() => setModal(null)} />}</DialogContent>
      </Dialog>
      <Dialog open={modal?.kind === 'plans'} onOpenChange={(o) => !o && setModal(null)}>
        <DialogContent className="sm:max-w-[600px]">{modal?.kind === 'plans' && <UserPlans user={modal.user} />}</DialogContent>
      </Dialog>
      <DeleteUser user={modal?.kind === 'delete' ? modal.user : null} onClose={() => setModal(null)} />
    </>
  )
}

function UserForm({ user, onDone }: { user?: AdminUser; onDone: () => void }) {
  const qc = useQueryClient()
  const plans = useQuery({ queryKey: ['plans'], queryFn: api.plans, enabled: !user })
  const [name, setName] = useState(user?.name ?? '')
  const [email, setEmail] = useState(user?.email ?? '')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>(user?.role ?? 'user')
  const [planId, setPlanId] = useState<string>('none')
  const save = useMutation({
    mutationFn: () =>
      user
        ? api.updateUser(user.id, { name, email, role })
        : api.createUser({ name, email, role, password: password || undefined, planId: planId === 'none' ? undefined : planId }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast.success(user ? 'Saved' : `${name} can now sign in`)
      onDone()
    },
  })
  return (
    <form
      className="space-y-5"
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold tracking-[-0.015em]">{user ? `Edit ${user.name}` : 'New user'}</DialogTitle>
        <DialogDescription>{user ? 'Change their details or role.' : 'They sign in with this email and password.'}</DialogDescription>
      </DialogHeader>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" htmlFor="u-name">
          <Input id="u-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Email" htmlFor="u-email">
          <Input id="u-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        {!user && (
          <Field label="Password" htmlFor="u-pass" hint="At least 10 characters. Leave empty to set one later.">
            <Input id="u-pass" type="text" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} className="font-mono" />
          </Field>
        )}
        <Field label="Role" htmlFor="u-role">
          <Select value={role} onValueChange={(r) => setRole(r as Role)}>
            <SelectTrigger id="u-role">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="user">User · their own bots only</SelectItem>
              <SelectItem value="admin">Admin · everything</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        {!user && (
          <Field label="Plan" htmlFor="u-plan" className="sm:col-span-2">
            <Select value={planId} onValueChange={setPlanId}>
              <SelectTrigger id="u-plan">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">No plan yet</SelectItem>
                {plans.data?.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name} · {p.maxBots} {p.maxBots === 1 ? 'bot' : 'bots'}, {mb(p.memoryMb)}, {cores(p.cpuMillicores)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        )}
      </div>
      <FormError error={save.error} />
      <DialogFooter className="gap-2">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending || !name || !email}>
          {user ? 'Save' : 'Create user'}
        </Button>
      </DialogFooter>
    </form>
  )
}

function PasswordForm({ user, onDone }: { user: AdminUser; onDone: () => void }) {
  const [password, setPassword] = useState('')
  const save = useMutation({
    mutationFn: () => api.resetPassword(user.id, password),
    onSuccess: () => {
      toast.success(`Password set. ${user.name} has been signed out everywhere.`)
      onDone()
    },
  })
  return (
    <form
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold tracking-[-0.015em]">Set a password</DialogTitle>
        <DialogDescription>For {user.email}. They are signed out of every session.</DialogDescription>
      </DialogHeader>
      <Field label="New password" htmlFor="np" hint="At least 10 characters. Share it with them privately.">
        <Input id="np" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus className="font-mono" autoComplete="new-password" />
      </Field>
      <FormError error={save.error} />
      <DialogFooter className="gap-2">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending || password.length < 10}>
          Set password
        </Button>
      </DialogFooter>
    </form>
  )
}

function UserPlans({ user }: { user: AdminUser }) {
  const qc = useQueryClient()
  const subs = useQuery({ queryKey: ['user-subs', user.id], queryFn: () => api.userSubscriptions(user.id) })
  const plans = useQuery({ queryKey: ['plans'], queryFn: api.plans })
  const [planId, setPlanId] = useState('')
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['user-subs', user.id] })
    qc.invalidateQueries({ queryKey: ['users'] })
    qc.invalidateQueries({ queryKey: ['plans'] })
  }
  const add = useMutation({ mutationFn: () => api.subscribe(user.id, planId), onSuccess: () => (setPlanId(''), refresh(), toast.success('Plan added')) })
  const status = useMutation({
    mutationFn: ({ id, s }: { id: string; s: 'active' | 'suspended' | 'terminated' }) => api.updateSubscription(id, { status: s }),
    onSuccess: refresh,
    onError: (e) => toast.error(e.message),
  })
  return (
    <div className="space-y-5">
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold tracking-[-0.015em]">{user.name}'s plans</DialogTitle>
        <DialogDescription>Customize limits for this customer only, suspend a plan to stop its bots, or terminate it to delete them.</DialogDescription>
      </DialogHeader>
      <div className="space-y-3">
        {subs.data?.length === 0 && <p className="rounded-[14px] bg-surface-2 px-4 py-6 text-center text-[14px] text-muted-foreground">No plans yet.</p>}
        {subs.data?.map((s) => (
          <SubscriptionRow key={s.id} sub={s} onStatus={(st) => status.mutate({ id: s.id, s: st })} onSaved={refresh} />
        ))}
      </div>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (planId) add.mutate()
        }}
      >
        <Select value={planId} onValueChange={setPlanId}>
          <SelectTrigger aria-label="Plan to add">
            <SelectValue placeholder="Add a plan…" />
          </SelectTrigger>
          <SelectContent>
            {plans.data?.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="submit" disabled={!planId || add.isPending}>
          <Plus strokeWidth={2.25} /> Add
        </Button>
      </form>
      <FormError error={add.error} />
    </div>
  )
}

const limitFields: { key: keyof Limits; label: string; unit?: 'cores' }[] = [
  { key: 'maxBots', label: 'Bots' },
  { key: 'memoryMb', label: 'Memory (MB)' },
  { key: 'cpuMillicores', label: 'CPU (cores)', unit: 'cores' },
  { key: 'diskMb', label: 'Disk (MB)' },
  { key: 'pidsMax', label: 'Processes per bot' },
]

function SubscriptionRow({ sub: s, onStatus, onSaved }: { sub: Subscription; onStatus: (st: 'active' | 'suspended' | 'terminated') => void; onSaved: () => void }) {
  const [editing, setEditing] = useState(false)
  const custom = Object.values(s.overrides).some((v) => v != null)
  const toInput = (k: keyof Limits) => {
    const v = s.overrides[k]
    if (v == null) return ''
    return String(k === 'cpuMillicores' ? v / 1000 : v)
  }
  const [vals, setVals] = useState<Record<keyof Limits, string>>(() => Object.fromEntries(limitFields.map((f) => [f.key, toInput(f.key)])) as Record<keyof Limits, string>)
  const [note, setNote] = useState(s.note)
  const save = useMutation({
    mutationFn: () => {
      const overrides = Object.fromEntries(
        limitFields.map((f) => {
          const raw = vals[f.key].trim()
          if (raw === '') return [f.key, null]
          const n = Number(raw)
          return [f.key, f.unit === 'cores' ? Math.round(n * 1000) : Math.round(n)]
        }),
      )
      return api.updateSubscription(s.id, { overrides, note })
    },
    onSuccess: () => {
      setEditing(false)
      onSaved()
      toast.success('Limits saved')
    },
  })
  return (
    <div className="rounded-[16px] border">
      <div className="flex flex-wrap items-center gap-3 px-4 py-3.5">
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-2 font-semibold">
            {s.plan.name}
            {custom && <span className="rounded-full bg-brand-soft px-2 py-0.5 text-[11.5px] font-semibold text-brand-text">Custom</span>}
          </p>
          <p className="mt-0.5 text-[13px] text-muted-foreground tabular-nums">
            {s.used.bots}/{s.limits.maxBots} bots · {mb(s.used.memoryMb)}/{mb(s.limits.memoryMb)} · {cores(s.used.cpuMillicores)}/{cores(s.limits.cpuMillicores)} · {mb(s.used.diskMb)}/{mb(s.limits.diskMb)}
          </p>
          {s.note && <p className="mt-1 text-[12.5px] text-faint-foreground italic">{s.note}</p>}
        </div>
        {s.status === 'active' ? <Pill tone="success">Active</Pill> : <Pill tone="orange">Suspended</Pill>}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="Plan actions">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="rounded-[14px] p-1.5">
            <DropdownMenuItem className="rounded-[10px]" onSelect={() => setEditing(true)}>
              <SlidersHorizontal /> Customize limits
            </DropdownMenuItem>
            <DropdownMenuItem className="rounded-[10px]" onSelect={() => onStatus(s.status === 'active' ? 'suspended' : 'active')}>
              {s.status === 'active' ? 'Suspend' : 'Reactivate'}
            </DropdownMenuItem>
            <DropdownMenuItem className="rounded-[10px]" variant="destructive" onSelect={() => onStatus('terminated')}>
              Terminate and delete bots
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {editing && (
        <form
          className="space-y-4 border-t bg-surface-2/40 px-4 py-4"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <p className="text-[13px] text-muted-foreground">Only for this customer. Leave a field empty to use the {s.plan.name} plan's value, shown faded.</p>
          <div className="grid gap-3 sm:grid-cols-3">
            {limitFields.map((f) => (
              <Field key={f.key} label={f.label} htmlFor={`ov-${s.id}-${f.key}`}>
                <Input
                  id={`ov-${s.id}-${f.key}`}
                  type="number"
                  step={f.unit === 'cores' ? 0.05 : 1}
                  value={vals[f.key]}
                  placeholder={String(f.unit === 'cores' ? s.plan[f.key] / 1000 : s.plan[f.key])}
                  onChange={(e) => setVals((v) => ({ ...v, [f.key]: e.target.value }))}
                />
              </Field>
            ))}
            <Field label="Note for admins" htmlFor={`ov-${s.id}-note`}>
              <Input id={`ov-${s.id}-note`} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Why this customer is different" />
            </Field>
          </div>
          <FormError error={save.error} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={() => setEditing(false)}>
              Cancel
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setVals(Object.fromEntries(limitFields.map((f) => [f.key, ''])) as Record<keyof Limits, string>)}>
              Reset to plan
            </Button>
            <Button type="submit" size="sm" disabled={save.isPending}>
              Save limits
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}

function DeleteUser({ user, onClose }: { user: AdminUser | null; onClose: () => void }) {
  const qc = useQueryClient()
  const del = useMutation({
    mutationFn: () => api.deleteUser(user!.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast(`${user!.name} was deleted`)
      onClose()
    },
    onError: (e) => toast.error(e.message),
  })
  return (
    <AlertDialog open={!!user} onOpenChange={(o) => !o && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="font-display text-[22px] font-bold">Delete {user?.name}?</AlertDialogTitle>
          <AlertDialogDescription>Their account, plans and API keys go. Users with bots cannot be deleted until the bots are.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel className="rounded-full">Cancel</AlertDialogCancel>
          <AlertDialogAction className="rounded-full bg-danger text-white hover:bg-danger/90" onClick={(e) => (e.preventDefault(), del.mutate())}>
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
