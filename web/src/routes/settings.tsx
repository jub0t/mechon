import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { type FormEvent, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Field, FormError } from '@/components/data/field'
import { Segmented } from '@/components/data/stat'
import { Reveal } from '@/components/motion/reveal'
import { Card, CardHeader } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type AuditEntry } from '@/lib/api'
import { ago } from '@/lib/format'

export function SettingsPage() {
  return (
    <>
      <Reveal>
        <PageHeader title="Settings" description="How your panel presents itself, and a record of every change made in it." />
      </Reveal>
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <Reveal delay={0.04}>
          <Branding />
        </Reveal>
        <Reveal delay={0.08}>
          <AuditLog />
        </Reveal>
      </div>
    </>
  )
}

function Branding() {
  const qc = useQueryClient()
  const s = useQuery({ queryKey: ['public-settings'], queryFn: api.publicSettings })
  const [brand, setBrand] = useState('')
  const [support, setSupport] = useState('')
  useEffect(() => {
    if (s.data) {
      setBrand(s.data.brandName)
      setSupport(s.data.supportUrl)
    }
  }, [s.data])
  const save = useMutation({
    mutationFn: () => api.updateSettings({ brandName: brand, supportUrl: support }),
    onSuccess: (d) => {
      qc.setQueryData(['public-settings'], d)
      toast.success('Saved')
    },
  })
  return (
    <Card className="px-6 py-5">
      <form
        className="space-y-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <p className="font-display text-[17px] font-bold">Branding</p>
        <Field label="Brand name" htmlFor="st-brand" hint="Your customers see it on the sign-in page and in the browser tab.">
          <Input id="st-brand" value={brand} onChange={(e) => setBrand(e.target.value)} placeholder="Mechon" />
        </Field>
        <Field label="Support link" htmlFor="st-support" hint="Where “Contact support” goes. https:// or mailto:.">
          <Input id="st-support" value={support} onChange={(e) => setSupport(e.target.value)} placeholder="https://discord.gg/your-server" />
        </Field>
        <FormError error={save.error} />
        <Button type="submit" disabled={save.isPending}>
          Save
        </Button>
      </form>
    </Card>
  )
}

const filters = [
  { value: '', label: 'All' },
  { value: 'bot.', label: 'Bots' },
  { value: 'user.', label: 'Users' },
  { value: 'plan.', label: 'Plans' },
  { value: 'node.', label: 'Nodes' },
  { value: 'webhook.', label: 'Webhooks' },
]

const verbs: Record<string, string> = {
  create: 'created',
  update: 'changed',
  delete: 'deleted',
  archive: 'archived',
  start: 'started',
  stop: 'stopped',
  restart: 'restarted',
  deploy: 'deployed',
  rollback: 'rolled back',
  env: 'changed the environment of',
  password: 'set the password of',
  suspend: 'suspended',
  unsuspend: 'unsuspended',
  token: 'issued a new token for',
}

function describe(a: AuditEntry) {
  const [kind, verb] = a.action.split('.')
  return `${verbs[verb] ?? verb} ${kind === 'apikey' ? 'API key' : kind}`
}

function AuditLog() {
  const [prefix, setPrefix] = useState('')
  const log = useInfiniteQuery({
    queryKey: ['audit', prefix],
    queryFn: ({ pageParam }) => api.audit(pageParam, prefix),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => (last.length === 100 ? last[last.length - 1].id : undefined),
  })
  const rows = log.data?.pages.flat() ?? []
  return (
    <Card className="overflow-hidden">
      <CardHeader title="Audit log" />
      <div className="no-scrollbar overflow-x-auto px-6 pb-4">
        <Segmented value={prefix} onChange={setPrefix} options={filters} />
      </div>
      {log.isPending ? (
        <div className="space-y-2 px-6 pb-5">
          <Skeleton className="h-12 rounded-[12px]" />
          <Skeleton className="h-12 rounded-[12px]" />
        </div>
      ) : rows.length === 0 ? (
        <p className="border-t px-6 py-10 text-center text-[14px] text-faint-foreground">Nothing recorded yet.</p>
      ) : (
        <ul>
          {rows.map((a) => (
            <li key={a.id} className="flex flex-wrap items-baseline gap-x-2 gap-y-1 border-t px-6 py-3 text-[14px]">
              <span className="font-semibold">{a.actorName}</span>
              <span className="text-muted-foreground">{describe(a)}</span>
              {a.targetName && <span className="font-semibold">{a.targetName}</span>}
              {a.viaApiKey && (
                <span className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 text-[11.5px] font-semibold text-muted-foreground">
                  <KeyRound className="size-3" /> API key
                </span>
              )}
              <span className="ml-auto text-[12.5px] text-faint-foreground" title={new Date(a.createdAt).toLocaleString()}>
                {ago(a.createdAt)} · {a.ip}
              </span>
            </li>
          ))}
        </ul>
      )}
      {log.hasNextPage && (
        <div className="border-t px-6 py-3">
          <Button variant="ghost" size="sm" onClick={() => log.fetchNextPage()} disabled={log.isFetchingNextPage}>
            Load older
          </Button>
        </div>
      )}
    </Card>
  )
}
