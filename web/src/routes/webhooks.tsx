import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, MoreHorizontal, Plus, RotateCw, Send, Trash2, Webhook as WebhookIcon } from 'lucide-react'
import { type FormEvent, useCallback, useState } from 'react'
import { toast } from 'sonner'
import { Field, FormError } from '@/components/data/field'
import { Pill } from '@/components/data/state-badge'
import { EmptyState } from '@/components/data/stat'
import { Reveal } from '@/components/motion/reveal'
import { Card } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useNewParam } from '@/hooks/use-new-param'
import { api, type Delivery, type Webhook } from '@/lib/api'
import { ago } from '@/lib/format'
import { cn } from '@/lib/utils'
import { CopyBlock } from './nodes'

export function WebhooksPage() {
  const qc = useQueryClient()
  const hooks = useQuery({ queryKey: ['webhooks'], queryFn: api.webhooks, refetchInterval: 15000 })
  const [editing, setEditing] = useState<Webhook | 'new' | null>(null)
  const [viewing, setViewing] = useState<Webhook | null>(null)
  const [secret, setSecret] = useState<string | null>(null)
  useNewParam(useCallback(() => setEditing('new'), []))
  const refresh = () => qc.invalidateQueries({ queryKey: ['webhooks'] })
  const toggle = useMutation({
    mutationFn: (h: Webhook) => api.updateWebhook(h.id, { url: h.url, description: h.description, events: h.events, enabled: !h.enabled }),
    onSuccess: refresh,
    onError: (e) => toast.error(e.message),
  })
  const test = useMutation({ mutationFn: (h: Webhook) => api.testWebhook(h.id), onSuccess: () => toast('Ping sent. Check the deliveries.') })
  const remove = useMutation({ mutationFn: (h: Webhook) => api.deleteWebhook(h.id), onSuccess: refresh })

  return (
    <>
      <Reveal>
        <PageHeader
          title="Webhooks"
          description="Tell your billing system and tools what happens: crashes, deploys, suspensions, nodes going offline. Every request is signed and retried for a day."
          actions={
            <Button onClick={() => setEditing('new')}>
              <Plus strokeWidth={2.25} /> Add endpoint
            </Button>
          }
        />
      </Reveal>

      {hooks.isPending ? (
        <Skeleton className="h-40 rounded-[20px]" />
      ) : !hooks.data?.endpoints.length ? (
        <Card>
          <EmptyState
            icon={<WebhookIcon className="size-6" strokeWidth={2.25} />}
            title="No endpoints yet"
            text="Add a URL that accepts POST requests. Paymenter, WHMCS and Stripe glue code can react to bot and subscription events from here."
            action={
              <Button onClick={() => setEditing('new')}>
                <Plus strokeWidth={2.25} /> Add endpoint
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {hooks.data.endpoints.map((h, i) => (
            <Reveal key={h.id} delay={0.04 * i}>
              <Card className={cn('flex flex-wrap items-center gap-4 px-6 py-4', !h.enabled && 'opacity-60')}>
                <button type="button" onClick={() => setViewing(h)} className="group min-w-0 flex-1 text-left">
                  <p className="flex items-center gap-2 truncate font-mono text-[14px] font-semibold">
                    {h.url}
                    <ChevronRight className="size-4 shrink-0 text-faint-foreground transition-transform group-hover:translate-x-0.5" />
                  </p>
                  <p className="mt-1 truncate text-[13px] text-muted-foreground">
                    {h.description ? `${h.description} · ` : ''}
                    {h.events.length ? `${h.events.length} events` : 'All events'} · added {ago(h.createdAt)}
                  </p>
                </button>
                {h.failed24h > 0 && <Pill tone="danger">{h.failed24h} failing</Pill>}
                <Switch checked={h.enabled} onCheckedChange={() => toggle.mutate(h)} aria-label={h.enabled ? 'Disable endpoint' : 'Enable endpoint'} />
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon" aria-label="Endpoint actions">
                      <MoreHorizontal className="size-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="min-w-48 rounded-[14px] p-1.5">
                    <DropdownMenuItem className="rounded-[10px]" onSelect={() => test.mutate(h)}>
                      <Send /> Send a test ping
                    </DropdownMenuItem>
                    <DropdownMenuItem className="rounded-[10px]" onSelect={() => setViewing(h)}>
                      <RotateCw /> Deliveries
                    </DropdownMenuItem>
                    <DropdownMenuItem className="rounded-[10px]" onSelect={() => setEditing(h)}>
                      <WebhookIcon /> Edit
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem className="rounded-[10px]" variant="destructive" onSelect={() => remove.mutate(h)}>
                      <Trash2 /> Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </Card>
            </Reveal>
          ))}
        </div>
      )}

      <Dialog open={editing !== null} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent className="sm:max-w-[600px]">
          {editing !== null && (
            <WebhookForm
              hook={editing === 'new' ? undefined : editing}
              events={hooks.data?.events ?? []}
              onDone={(s) => {
                setEditing(null)
                refresh()
                if (s) setSecret(s)
              }}
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={!!secret} onOpenChange={(o) => !o && setSecret(null)}>
        <DialogContent className="sm:max-w-[640px]">
          <DialogHeader>
            <DialogTitle className="font-display text-[22px] font-bold">Copy the signing secret</DialogTitle>
            <DialogDescription>Shown only now. Use it to check that requests really come from this panel.</DialogDescription>
          </DialogHeader>
          {secret && (
            <div className="space-y-4">
              <CopyBlock label="Signing secret" text={secret} />
              <CopyBlock
                label="Verify a request (Node.js)"
                text={`// header Mechon-Signature: t=<unix>,v1=<hex>
const [t, v1] = sig.split(',').map((p) => p.split('=')[1])
const expected = crypto.createHmac('sha256', SECRET).update(\`\${t}.\${rawBody}\`).digest('hex')
const ok = crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(v1)) && Date.now() / 1000 - t < 300`}
              />
            </div>
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={!!viewing} onOpenChange={(o) => !o && setViewing(null)}>
        <DialogContent className="max-h-[88svh] overflow-y-auto sm:max-w-[720px]">{viewing && <Deliveries hook={viewing} />}</DialogContent>
      </Dialog>
    </>
  )
}

function WebhookForm({ hook, events, onDone }: { hook?: Webhook; events: string[]; onDone: (secret?: string) => void }) {
  const [url, setUrl] = useState(hook?.url ?? '')
  const [description, setDescription] = useState(hook?.description ?? '')
  const [picked, setPicked] = useState<string[]>(hook?.events ?? [])
  const save = useMutation({
    mutationFn: async () => {
      const body = { url, description, events: picked, enabled: hook?.enabled ?? true }
      if (hook) {
        await api.updateWebhook(hook.id, body)
        return undefined
      }
      return (await api.createWebhook(body)).secret
    },
    onSuccess: (s) => onDone(s),
  })
  const groups = events.reduce<Record<string, string[]>>((acc, e) => {
    const g = e.split('.')[0]
    ;(acc[g] ??= []).push(e)
    return acc
  }, {})
  return (
    <form
      className="space-y-5"
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold">{hook ? 'Edit endpoint' : 'Add an endpoint'}</DialogTitle>
        <DialogDescription>Mechon POSTs JSON to this URL. Answer with any 2xx within 15 seconds.</DialogDescription>
      </DialogHeader>
      <Field label="URL" htmlFor="w-url">
        <Input id="w-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://billing.example.com/hooks/mechon" className="font-mono text-[14px]" autoFocus />
      </Field>
      <Field label="Description" htmlFor="w-desc">
        <Input id="w-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Paymenter" />
      </Field>
      <div>
        <div className="mb-2 flex items-baseline justify-between">
          <p className="text-[13.5px] font-semibold">Events</p>
          <button type="button" className="text-[12.5px] font-semibold text-brand-text" onClick={() => setPicked([])}>
            {picked.length ? 'Send all events' : 'All events selected'}
          </button>
        </div>
        <div className="space-y-3 rounded-[14px] bg-surface-2/60 p-3.5">
          {Object.entries(groups).map(([g, list]) => (
            <div key={g} className="flex flex-wrap items-center gap-1.5">
              <span className="w-24 text-[12px] font-semibold tracking-[0.04em] text-faint-foreground uppercase">{g}</span>
              {list.map((e) => {
                const on = picked.includes(e)
                return (
                  <button
                    key={e}
                    type="button"
                    onClick={() => setPicked((p) => (on ? p.filter((x) => x !== e) : [...p, e]))}
                    className={cn(
                      'h-8 rounded-full border px-3 font-mono text-[12px] transition-colors',
                      on ? 'border-brand bg-brand-soft text-brand-text' : 'border-transparent bg-surface text-muted-foreground hover:text-foreground',
                    )}
                  >
                    {e.split('.')[1]}
                  </button>
                )
              })}
            </div>
          ))}
        </div>
      </div>
      <FormError error={save.error} />
      <DialogFooter className="gap-2">
        <Button type="button" variant="outline" onClick={() => onDone()}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending || !url}>
          {hook ? 'Save' : 'Add endpoint'}
        </Button>
      </DialogFooter>
    </form>
  )
}

function Deliveries({ hook }: { hook: Webhook }) {
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ['deliveries', hook.id], queryFn: () => api.deliveries(hook.id), refetchInterval: 3000 })
  const [open, setOpen] = useState<string | null>(null)
  const retry = useMutation({ mutationFn: (d: Delivery) => api.retryDelivery(d.id), onSuccess: () => qc.invalidateQueries({ queryKey: ['deliveries', hook.id] }) })
  return (
    <div className="space-y-4">
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold">Deliveries</DialogTitle>
        <DialogDescription className="font-mono text-[12.5px]">{hook.url}</DialogDescription>
      </DialogHeader>
      {list.data?.length === 0 && <p className="rounded-[14px] bg-surface-2 px-4 py-8 text-center text-[14px] text-muted-foreground">Nothing sent yet. Send a test ping from the menu.</p>}
      <ul className="space-y-2">
        {list.data?.map((d) => (
          <li key={d.id} className="rounded-[14px] border">
            <button type="button" onClick={() => setOpen(open === d.id ? null : d.id)} className="flex w-full flex-wrap items-center gap-3 px-4 py-3 text-left">
              <span className="font-mono text-[13px] font-semibold">{d.eventType}</span>
              <span className="text-[12.5px] text-muted-foreground">
                {ago(d.createdAt)} · {d.attempts} {d.attempts === 1 ? 'attempt' : 'attempts'}
                {d.lastStatusCode ? ` · HTTP ${d.lastStatusCode}` : ''}
              </span>
              <span className="ml-auto">
                {d.status === 'delivered' ? <Pill tone="success">Delivered</Pill> : d.status === 'failed' ? <Pill tone="danger">Failed</Pill> : <Pill tone="orange" pulse>Retrying</Pill>}
              </span>
            </button>
            {open === d.id && (
              <div className="space-y-3 border-t px-4 py-3">
                {d.lastError && <p className="text-[13px] text-danger">{d.lastError}</p>}
                <pre className="max-h-64 overflow-auto rounded-[12px] bg-[oklch(0.1_0.004_285)] px-3.5 py-3 font-mono text-[12px] text-[oklch(0.88_0.008_90)]">
                  {JSON.stringify(d.payload, null, 2)}
                </pre>
                {d.status !== 'delivered' && (
                  <Button size="sm" variant="outline" onClick={() => retry.mutate(d)} disabled={retry.isPending}>
                    <RotateCw className="size-4" /> Retry now
                  </Button>
                )}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
