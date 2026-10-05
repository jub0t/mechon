import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, KeyRound, MoreHorizontal, Plus, Server, Settings2, Trash2, Wrench } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { toast } from 'sonner'
import { Meter } from '@/components/data/meter'
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
import { type AgentSetup, api, type Node, type NodeInput } from '@/lib/api'
import { ago, bytes, cores, mb, pct } from '@/lib/format'

export function NodesPage() {
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes, refetchInterval: 5000 })
  const [editing, setEditing] = useState<Node | 'new' | null>(null)
  const [setup, setSetup] = useState<{ name: string; setup: AgentSetup } | null>(null)
  const qc = useQueryClient()
  const rotate = useMutation({
    mutationFn: (n: Node) => api.rotateNodeToken(n.id).then((s) => ({ name: n.name, setup: s })),
    onSuccess: (s) => setSetup(s),
    onError: (e) => toast.error(e.message),
  })
  const remove = useMutation({
    mutationFn: (n: Node) => api.deleteNode(n.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['nodes'] }),
    onError: (e) => toast.error(e.message),
  })

  return (
    <>
      <Reveal>
        <PageHeader
          title="Nodes"
          description="The servers your bots run on. Each runs the Mechon agent next to Docker and dials out to this panel, so no ports need opening."
          actions={
            <Button onClick={() => setEditing('new')}>
              <Plus strokeWidth={2.25} /> Add node
            </Button>
          }
        />
      </Reveal>

      {nodes.isPending ? (
        <div className="grid gap-5 lg:grid-cols-2">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-72 rounded-[20px]" />
          ))}
        </div>
      ) : nodes.data?.length ? (
        <div className="grid gap-5 lg:grid-cols-2">
          {nodes.data.map((n, i) => (
            <Reveal key={n.id} delay={0.04 * i}>
              <NodeCard
                node={n}
                actions={
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" aria-label={`Actions for ${n.name}`}>
                        <MoreHorizontal className="size-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="min-w-52 rounded-[14px] p-1.5">
                      <DropdownMenuItem className="rounded-[10px]" onSelect={() => setEditing(n)}>
                        <Settings2 /> Capacity and settings
                      </DropdownMenuItem>
                      <DropdownMenuItem className="rounded-[10px]" onSelect={() => rotate.mutate(n)}>
                        <KeyRound /> New agent token
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem className="rounded-[10px]" variant="destructive" onSelect={() => remove.mutate(n)}>
                        <Trash2 /> Remove node
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                }
              />
            </Reveal>
          ))}
        </div>
      ) : (
        <Card>
          <EmptyState
            icon={<Server className="size-6" strokeWidth={2.25} />}
            title="No nodes yet"
            text="Add a server with Docker installed. You get a one-line command that starts the agent; it connects out to this panel."
            action={
              <Button onClick={() => setEditing('new')}>
                <Plus strokeWidth={2.25} /> Add node
              </Button>
            }
          />
        </Card>
      )}

      <Dialog open={editing !== null} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent className="sm:max-w-[540px]">
          {editing !== null && (
            <NodeForm
              key={editing === 'new' ? 'new' : editing.id}
              node={editing === 'new' ? undefined : editing}
              onDone={(s) => {
                setEditing(null)
                if (s) setSetup(s)
              }}
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={setup !== null} onOpenChange={(o) => !o && setSetup(null)}>
        <DialogContent className="sm:max-w-[640px]">{setup && <SetupView name={setup.name} setup={setup.setup} />}</DialogContent>
      </Dialog>
    </>
  )
}

function NodeCard({ node: n, actions }: { node: Node; actions: React.ReactNode }) {
  const memSell = n.capacity.memoryMb * n.overcommit
  const cpuSell = n.capacity.cpuMillicores * n.overcommit
  return (
    <Card className="flex h-full flex-col px-6 py-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="flex size-11 items-center justify-center rounded-[13px] bg-surface-2 text-muted-foreground">
            <Server className="size-5" strokeWidth={2.25} />
          </span>
          <div>
            <p className="font-display text-[19px] font-bold tracking-[-0.01em]">{n.name}</p>
            <p className="text-[13px] text-muted-foreground">{[n.region, n.host?.hostname].filter(Boolean).join(' · ') || 'Waiting for the agent'}</p>
          </div>
        </div>
        <div className="flex items-center gap-1">
          {n.maintenance ? (
            <Pill tone="orange">Maintenance</Pill>
          ) : n.online ? (
            <Pill tone="success" pulse>
              Online
            </Pill>
          ) : (
            <Pill tone={n.lastSeenAt ? 'danger' : 'neutral'}>{n.lastSeenAt ? `Offline · ${ago(n.lastSeenAt)}` : 'Never connected'}</Pill>
          )}
          {actions}
        </div>
      </div>

      <div className="mt-6 grid gap-x-8 gap-y-5 sm:grid-cols-2">
        <Meter label="Memory sold" value={n.allocated.memoryMb} max={memSell} display={`${mb(n.allocated.memoryMb)} / ${mb(memSell)}`} />
        <Meter label="CPU sold" value={n.allocated.cpuMillicores} max={cpuSell} display={`${cores(n.allocated.cpuMillicores)} / ${cores(cpuSell)}`} />
        <Meter label="Disk sold" value={n.allocated.diskMb} max={n.capacity.diskMb} display={`${mb(n.allocated.diskMb)} / ${mb(n.capacity.diskMb)}`} />
        {n.usage && n.host ? (
          <Meter label="Live CPU" value={n.usage.cpuPercent} max={100} display={pct(n.usage.cpuPercent)} />
        ) : (
          <div className="text-[13px] text-faint-foreground">Live usage appears when the agent is connected.</div>
        )}
      </div>

      <div className="mt-auto flex flex-wrap gap-x-5 gap-y-1 border-t pt-4 mt-6 text-[12.5px] text-muted-foreground">
        <span>
          <b className="font-semibold text-foreground tabular-nums">{n.botCount}</b> {n.botCount === 1 ? 'bot' : 'bots'}
        </span>
        {n.host && (
          <>
            <span>
              {n.host.cpus} CPUs · {bytes(n.host.memoryBytes)} RAM
            </span>
            <span>Docker {n.host.dockerVersion}</span>
            {n.host.runsc && <span className="font-semibold text-brand-text">gVisor ready</span>}
          </>
        )}
        {n.overcommit > 1 && <span>{n.overcommit}× overcommit</span>}
        {n.agentVersion && <span className="ml-auto font-mono">agent {n.agentVersion}</span>}
      </div>
    </Card>
  )
}

const blank: NodeInput = { name: '', region: '', memoryMb: 8192, cpuMillicores: 4000, diskMb: 102400, overcommit: 1, maintenance: false }

function NodeForm({ node, onDone }: { node?: Node; onDone: (s?: { name: string; setup: AgentSetup }) => void }) {
  const qc = useQueryClient()
  const [v, setV] = useState<NodeInput>(
    node ? { name: node.name, region: node.region, ...node.capacity, overcommit: node.overcommit, maintenance: node.maintenance } : blank,
  )
  const set = <K extends keyof NodeInput>(k: K, val: NodeInput[K]) => setV((s) => ({ ...s, [k]: val }))
  const save = useMutation({
    mutationFn: async () => {
      if (node) {
        await api.updateNode(node.id, v)
        return undefined
      }
      const r = await api.createNode(v)
      return { name: r.node.name, setup: r.setup }
    },
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['nodes'] })
      onDone(s)
    },
  })
  const fill = () =>
    node?.host &&
    setV((s) => ({
      ...s,
      memoryMb: Math.max(256, Math.floor(node.host!.memoryBytes / 2 ** 20) - 2048),
      cpuMillicores: node.host!.cpus * 1000,
      diskMb: Math.max(1024, Math.floor((node.host!.diskBytes / 2 ** 20) * 0.85)),
    }))
  return (
    <form
      className="space-y-5"
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold tracking-[-0.015em]">{node ? node.name : 'Add a node'}</DialogTitle>
        <DialogDescription>Capacity is what you sell from this server, not what the hardware has. Leave room for the system.</DialogDescription>
      </DialogHeader>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" htmlFor="n-name">
          <Input id="n-name" value={v.name} onChange={(e) => set('name', e.target.value)} placeholder="fra-1" autoFocus />
        </Field>
        <Field label="Region" htmlFor="n-region">
          <Input id="n-region" value={v.region} onChange={(e) => set('region', e.target.value)} placeholder="Falkenstein" />
        </Field>
        <Field label="Memory to sell (MB)" htmlFor="n-mem">
          <Input id="n-mem" type="number" min={256} step={256} value={v.memoryMb} onChange={(e) => set('memoryMb', Number(e.target.value))} />
        </Field>
        <Field label="CPU to sell (cores)" htmlFor="n-cpu">
          <Input id="n-cpu" type="number" min={0.25} step={0.25} value={v.cpuMillicores / 1000} onChange={(e) => set('cpuMillicores', Math.round(Number(e.target.value) * 1000))} />
        </Field>
        <Field label="Disk to sell (MB)" htmlFor="n-disk">
          <Input id="n-disk" type="number" min={1024} step={1024} value={v.diskMb} onChange={(e) => set('diskMb', Number(e.target.value))} />
        </Field>
        <Field label="Overcommit" htmlFor="n-oc" hint="Memory and CPU only. Bots idle a lot; 1.5× is common.">
          <Input id="n-oc" type="number" min={1} max={10} step={0.1} value={v.overcommit} onChange={(e) => set('overcommit', Number(e.target.value))} />
        </Field>
      </div>
      {node && (
        <div className="flex items-center justify-between gap-4 rounded-[14px] bg-surface-2 px-4 py-3.5">
          <div>
            <p className="flex items-center gap-2 text-[14px] font-semibold">
              <Wrench className="size-4" /> Maintenance mode
            </p>
            <p className="mt-0.5 text-[12.5px] text-muted-foreground">No new bots land here. Running bots keep running.</p>
          </div>
          <Switch checked={v.maintenance} onCheckedChange={(c) => set('maintenance', c)} aria-label="Maintenance mode" />
        </div>
      )}
      {node?.host && (
        <Button type="button" variant="link" className="h-auto px-0" onClick={fill}>
          Use what the server reports, minus a margin for the system
        </Button>
      )}
      <FormError error={save.error} />
      <DialogFooter className="gap-2">
        <Button type="button" variant="outline" onClick={() => onDone()}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending || !v.name}>
          {node ? 'Save' : 'Add node'}
        </Button>
      </DialogFooter>
    </form>
  )
}

function SetupView({ name, setup }: { name: string; setup: AgentSetup }) {
  const unit = `[Unit]
Description=Mechon agent
After=docker.service network-online.target
Requires=docker.service

[Service]
Environment=MECHON_PANEL_URL=${setup.panelUrl}
Environment=MECHON_NODE_TOKEN=${setup.token}
ExecStart=/usr/local/bin/mechon-agent
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target`
  return (
    <div className="space-y-5">
      <DialogHeader>
        <DialogTitle className="font-display text-[22px] font-bold tracking-[-0.015em]">Start the agent on {name}</DialogTitle>
        <DialogDescription>
          On the server, with Docker installed and <code className="font-mono text-[13px]">mechon-agent</code> in your PATH, run this as root. The token is shown only now.
        </DialogDescription>
      </DialogHeader>
      <CopyBlock label="Quick start" text={setup.command} />
      <CopyBlock label="Or as a systemd service · /etc/systemd/system/mechon-agent.service" text={unit} />
      <p className="text-[13px] text-muted-foreground">The node turns online on this page as soon as the agent connects.</p>
    </div>
  )
}

export function CopyBlock({ label, text }: { label: string; text: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div>
      <p className="mb-2 text-[12px] font-semibold tracking-[0.04em] text-faint-foreground uppercase">{label}</p>
      <div className="group relative rounded-[14px] bg-[oklch(0.1_0.004_285)] text-[oklch(0.92_0.008_90)]">
        <pre className="no-scrollbar overflow-x-auto px-4 py-3.5 pr-14 font-mono text-[12.5px] leading-relaxed whitespace-pre">{text}</pre>
        <button
          type="button"
          onClick={() => {
            navigator.clipboard.writeText(text).then(() => {
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            })
          }}
          className="absolute top-2.5 right-2.5 inline-flex size-8 items-center justify-center rounded-[9px] bg-white/10 text-white/80 transition-colors hover:bg-white/20 hover:text-white"
          aria-label="Copy"
        >
          {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
        </button>
      </div>
    </div>
  )
}
