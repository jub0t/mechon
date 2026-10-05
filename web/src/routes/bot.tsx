import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowLeft,
  CloudUpload,
  Download,
  Eye,
  EyeOff,
  FileArchive,
  History,
  Loader2,
  Lock,
  Play,
  Plus,
  RotateCcw,
  RotateCw,
  Square,
  Terminal,
  Trash2,
  Unlock,
  WifiOff,
} from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { type DragEvent, type FormEvent, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { toast } from 'sonner'
import { Sparkline } from '@/components/data/chart'
import { TimeChart } from '@/components/data/time-chart'
import { Field, FormError } from '@/components/data/field'
import { BotStateBadge, DeployBadge, Pill } from '@/components/data/state-badge'
import { EmptyState, Segmented } from '@/components/data/stat'
import { Reveal } from '@/components/motion/reveal'
import { Card, CardHeader } from '@/components/page/card'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { ApiError, api, type Bot, type Deploy, type EnvVar, type LogLine } from '@/lib/api'
import { useBotStream } from '@/lib/bot-stream'
import { ago, bytes, clock, cores, duration, mb, pct } from '@/lib/format'
import { cn } from '@/lib/utils'
import { NotFoundPage } from './not-found'
import { TemplateMark } from './bots'

type Tab = 'console' | 'deploys' | 'metrics' | 'env' | 'settings'

export function BotPage() {
  const { id = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as Tab) || 'console'
  const setTab = (t: Tab) => setParams(t === 'console' ? {} : { tab: t }, { replace: true })
  const bot = useQuery({ queryKey: ['bot', id], queryFn: () => api.bot(id), refetchInterval: 15000 })
  const stream = useBotStream(id)

  if (bot.isError && bot.error instanceof ApiError && bot.error.status === 404) return <NotFoundPage />
  if (!bot.data)
    return (
      <div className="space-y-5">
        <Skeleton className="h-24 rounded-[20px]" />
        <Skeleton className="h-[420px] rounded-[20px]" />
      </div>
    )
  const b = bot.data
  const state = stream.live?.state ?? b.state
  const latest = stream.points[stream.points.length - 1] ?? b.usage

  return (
    <>
      <Reveal>
        <Link to="/bots" className="mb-5 inline-flex items-center gap-1.5 text-[13.5px] font-semibold text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Bots
        </Link>
        <div className="mb-8 flex flex-wrap items-start justify-between gap-5">
          <div className="flex items-center gap-4">
            <TemplateMark id={b.template} className="size-14 rounded-[16px] text-[14px]" />
            <div>
              <div className="flex flex-wrap items-center gap-3">
                <h1 className="text-title text-[clamp(1.8rem,1.3rem+1.4vw,2.4rem)]">{b.name}</h1>
                <BotStateBadge state={state} />
              </div>
              <p className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-[13.5px] text-muted-foreground">
                <span>{b.node.name}</span>
                <span className="font-mono text-[12.5px]">uid {b.uid}</span>
                {state === 'running' && <span>up {duration(b.stateChangedAt)}</span>}
                {b.restarts > 0 && <span>{b.restarts} restarts</span>}
                <span>{b.owner.email}</span>
              </p>
            </div>
          </div>
          <Actions bot={b} state={state} />
        </div>
      </Reveal>

      {!stream.nodeOnline && (
        <div className="mb-5 flex items-center gap-3 rounded-[16px] bg-orange-soft px-4 py-3 text-[14px] font-medium text-orange-text">
          <WifiOff className="size-4 shrink-0" /> The server this bot runs on is offline. Changes apply when it reconnects.
        </div>
      )}
      {b.error && (state === 'crashed' || state === 'stopped') && (
        <div className="mb-5 rounded-[16px] bg-danger-soft px-4 py-3 text-[14px] font-medium text-danger">
          {b.error}
          {b.exitCode != null && <span className="ml-2 font-mono text-[13px] opacity-80">exit {b.exitCode}</span>}
        </div>
      )}

      <Reveal delay={0.05}>
        <div className="mb-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <LiveTile
            label="Memory"
            value={latest ? bytes(latest.memoryBytes) : '—'}
            hint={`of ${mb(b.limits.memoryMb)}`}
            points={stream.points.map((p) => p.memoryBytes)}
            max={b.limits.memoryMb * 2 ** 20}
          />
          <LiveTile label="CPU" value={latest ? pct(latest.cpuPercent) : '—'} hint={`of ${cores(b.limits.cpuMillicores)}`} points={stream.points.map((p) => p.cpuPercent)} max={100} />
          <LiveTile label="Disk" value={latest ? bytes(latest.diskBytes) : '—'} hint={`of ${mb(b.limits.diskMb)}`} points={stream.points.map((p) => p.diskBytes)} />
          <LiveTile
            label="Network"
            value={latest ? `${bytes(latest.netRxBytes)} in` : '—'}
            hint={latest ? `${bytes(latest.netTxBytes)} out since start` : 'Starts counting when the bot runs'}
            points={stream.points.map((p) => p.netRxBytes)}
          />
        </div>
      </Reveal>

      <Segmented<Tab>
        className="mb-5"
        value={tab}
        onChange={setTab}
        options={[
          { value: 'console', label: 'Console' },
          { value: 'deploys', label: 'Deploys' },
          { value: 'metrics', label: 'Metrics' },
          { value: 'env', label: 'Environment' },
          { value: 'settings', label: 'Settings' },
        ]}
      />

      {tab === 'console' && <Console logs={stream.logs} connected={stream.connected} onClear={() => stream.setLogs([])} botName={b.name} />}
      {tab === 'deploys' && <Deploys bot={b} live={stream.deploy} />}
      {tab === 'metrics' && <Metrics bot={b} />}
      {tab === 'env' && <Environment bot={b} />}
      {tab === 'settings' && <Settings bot={b} />}
    </>
  )
}

function LiveTile({ label, value, hint, points, max }: { label: string; value: string; hint: string; points: number[]; max?: number }) {
  return (
    <div className="rounded-[20px] border bg-surface px-5 py-4">
      <p className="text-[13px] font-semibold text-muted-foreground">{label}</p>
      <div className="mt-2 flex items-end justify-between gap-3">
        <p className="font-display text-[24px] leading-none font-bold tracking-[-0.02em] whitespace-nowrap tabular-nums">{value}</p>
        <Sparkline points={points} max={max} className="w-[88px]" />
      </div>
      <p className="mt-2.5 text-[12.5px] text-faint-foreground">{hint}</p>
    </div>
  )
}

function Actions({ bot, state }: { bot: Bot; state: string }) {
  const qc = useQueryClient()
  const act = useMutation({
    mutationFn: (a: 'start' | 'stop' | 'restart') => api.botAction(bot.id, a),
    onSuccess: (b, a) => {
      qc.setQueryData(['bot', bot.id], b)
      toast(a === 'stop' ? 'Stopping…' : a === 'start' ? 'Starting…' : 'Restarting…')
    },
    onError: (e) => toast.error(e.message),
  })
  const running = bot.desired === 'running'
  const busy = act.isPending
  return (
    <div className="flex items-center gap-2">
      {running ? (
        <Button variant="outline" onClick={() => act.mutate('stop')} disabled={busy}>
          <Square className="size-4" strokeWidth={2.5} /> Stop
        </Button>
      ) : (
        <Button variant="brand" onClick={() => act.mutate('start')} disabled={busy}>
          <Play className="size-4" strokeWidth={2.5} /> Start
        </Button>
      )}
      <Button variant={running ? 'default' : 'outline'} onClick={() => act.mutate('restart')} disabled={busy || state === 'installing'}>
        <RotateCw className={cn('size-4', act.isPending && act.variables === 'restart' && 'animate-spin')} strokeWidth={2.5} /> Restart
      </Button>
    </div>
  )
}

// ---------- Console ----------

const streamColor: Record<LogLine['stream'], string> = {
  stdout: 'text-[oklch(0.9_0.008_90)]',
  stderr: 'text-[oklch(0.75_0.16_25)]',
  system: 'text-[oklch(0.78_0.13_300)]',
}

function Console({ logs, connected, onClear, botName }: { logs: LogLine[]; connected: boolean; onClear: () => void; botName: string }) {
  const box = useRef<HTMLDivElement>(null)
  const [follow, setFollow] = useState(true)
  const [filter, setFilter] = useState('')
  const [times, setTimes] = useState(true)
  const shown = useMemo(() => (filter ? logs.filter((l) => l.text.toLowerCase().includes(filter.toLowerCase())) : logs), [logs, filter])

  useLayoutEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [shown, follow])

  const download = () => {
    const text = logs.map((l) => `${new Date(l.t).toISOString()} [${l.stream}] ${l.text}`).join('\n')
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    a.download = `${botName}-${new Date().toISOString().slice(0, 19)}.log`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <span className="mr-auto flex items-center gap-2 text-[13px] font-semibold text-muted-foreground">
          <Terminal className="size-4" />
          {connected ? (
            <span className="flex items-center gap-1.5">
              <span className="relative inline-flex size-2">
                <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-60" />
                <span className="relative size-2 rounded-full bg-success" />
              </span>
              Live
            </span>
          ) : (
            'Reconnecting…'
          )}
        </span>
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter lines" className="h-9 w-48 rounded-full text-[13.5px]" />
        <Button variant="ghost" size="sm" onClick={() => setTimes((t) => !t)}>
          {times ? 'Hide' : 'Show'} times
        </Button>
        <Button variant="ghost" size="icon" onClick={download} aria-label="Download logs">
          <Download className="size-4" />
        </Button>
        <Button variant="ghost" size="sm" onClick={onClear}>
          Clear
        </Button>
      </div>
      <div className="relative">
        <div
          ref={box}
          onScroll={(e) => {
            const el = e.currentTarget
            setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
          }}
          className="h-[min(560px,60svh)] overflow-y-auto bg-[oklch(0.1_0.004_285)] px-4 py-3 font-mono text-[12.5px] leading-[1.65]"
        >
          {shown.length === 0 ? (
            <p className="py-16 text-center font-sans text-[14px] text-[oklch(0.6_0.008_285)]">
              {filter ? 'No lines match.' : 'No output yet. Deploy code and start the bot to see its logs here.'}
            </p>
          ) : (
            shown.map((l, i) => (
              <div key={i} className={cn('flex gap-3 whitespace-pre-wrap break-all', streamColor[l.stream])}>
                {times && <span className="shrink-0 text-[oklch(0.5_0.008_285)] select-none">{clock(l.t)}</span>}
                <span>{l.text}</span>
              </div>
            ))
          )}
        </div>
        <AnimatePresence>
          {!follow && (
            <motion.button
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 8 }}
              onClick={() => setFollow(true)}
              className="absolute right-4 bottom-4 inline-flex h-9 items-center gap-1.5 rounded-full bg-white px-3.5 text-[13px] font-semibold text-black shadow-lg"
            >
              <ArrowDown className="size-4" /> Latest
            </motion.button>
          )}
        </AnimatePresence>
      </div>
    </Card>
  )
}

// ---------- Deploys ----------

function Deploys({ bot, live }: { bot: Bot; live: ReturnType<typeof useBotStream>['deploy'] }) {
  const qc = useQueryClient()
  const deploys = useQuery({ queryKey: ['deploys', bot.id], queryFn: () => api.deploys(bot.id), refetchInterval: (q) => (q.state.data?.some((d) => ['queued', 'fetching', 'installing'].includes(d.status)) ? 2000 : false) })
  const [viewing, setViewing] = useState<string | null>(null)
  const rollback = useMutation({
    mutationFn: (d: Deploy) => api.rollback(bot.id, d.id),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ['deploys', bot.id] })
      toast(`Rolling back as deploy #${d.number}`)
    },
    onError: (e) => toast.error(e.message),
  })
  const activeLive = live && deploys.data?.find((d) => d.id === live.id)

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
      <div className="space-y-5">
        <DeploySource bot={bot} last={deploys.data?.find((d) => d.source === 'git' || (d.source === 'rollback' && d.gitUrl))} onDone={() => qc.invalidateQueries({ queryKey: ['deploys', bot.id] })} />
        {live && (live.phase === 'fetching' || live.phase === 'installing' || live.lines.length > 0) && (
          <Card className="overflow-hidden">
            <CardHeader
              title={`Deploy ${activeLive ? `#${activeLive.number}` : ''}`}
              aside={live.phase === 'live' ? <Pill tone="success">Live</Pill> : live.phase === 'failed' ? <Pill tone="danger">Failed</Pill> : <Pill tone="orange" pulse>{live.phase === 'fetching' ? 'Fetching' : 'Installing'}</Pill>}
            />
            <pre className="mx-4 mb-4 max-h-64 overflow-y-auto rounded-[12px] bg-[oklch(0.1_0.004_285)] px-3.5 py-3 font-mono text-[12px] leading-relaxed whitespace-pre-wrap text-[oklch(0.88_0.008_90)]">
              {live.lines.map((l) => l.text).join('\n') || 'Waiting for output…'}
              {live.error && `\n\n${live.error}`}
            </pre>
          </Card>
        )}
      </div>

      <Card className="overflow-hidden">
        <CardHeader title="History" aside={<History className="size-4 text-faint-foreground" />} />
        {deploys.isPending ? (
          <div className="space-y-2 px-6 pb-5">
            <Skeleton className="h-14 rounded-[12px]" />
            <Skeleton className="h-14 rounded-[12px]" />
          </div>
        ) : !deploys.data?.length ? (
          <EmptyState title="Nothing deployed yet" text="Upload a .zip or .tar.gz of your bot's folder. Dependencies install on the server." />
        ) : (
          <ul>
            {deploys.data.map((d) => (
              <li key={d.id} className="flex flex-wrap items-center gap-3 border-t px-6 py-3.5">
                <span className="font-display text-[17px] font-bold text-faint-foreground tabular-nums">#{d.number}</span>
                <div className="min-w-0 flex-1">
                  <p className="flex items-center gap-2 text-[14px] font-semibold">
                    {d.source === 'rollback' ? 'Rollback' : d.source === 'api' ? 'API deploy' : d.source === 'git' ? 'Git' : 'Upload'}
                    {d.gitRef && (
                      <span className="font-mono text-[12.5px] font-medium text-muted-foreground">
                        {d.gitRef} @ {d.gitCommit?.slice(0, 7)}
                      </span>
                    )}
                    {d.current && <span className="rounded-full bg-brand-soft px-2 py-0.5 text-[11.5px] font-semibold text-brand-text">current</span>}
                  </p>
                  <p className="text-[12.5px] text-muted-foreground">
                    {d.createdBy ?? 'API'} · {ago(d.createdAt)} · {bytes(d.bytes)} · <span className="font-mono">{d.sha256.slice(0, 8)}</span>
                  </p>
                  {d.error && <p className="mt-1 text-[12.5px] text-danger">{d.error}</p>}
                </div>
                <DeployBadge status={d.status} />
                <div className="flex gap-1">
                  <Button variant="ghost" size="icon-sm" onClick={() => setViewing(d.id)} aria-label={`Build log for deploy ${d.number}`}>
                    <FileArchive className="size-4" />
                  </Button>
                  {!d.current && (d.status === 'live' || d.status === 'superseded') && (
                    <Button variant="ghost" size="icon-sm" onClick={() => rollback.mutate(d)} aria-label={`Roll back to deploy ${d.number}`}>
                      <RotateCcw className="size-4" />
                    </Button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <DeployLog botId={bot.id} deployId={viewing} onClose={() => setViewing(null)} />
    </div>
  )
}

function DeploySource({ bot, last, onDone }: { bot: Bot; last?: Deploy; onDone: () => void }) {
  const [mode, setMode] = useState<'upload' | 'git'>(last ? 'git' : 'upload')
  return (
    <div className="space-y-3">
      <Segmented
        value={mode}
        onChange={setMode}
        options={[
          { value: 'upload', label: 'Upload' },
          { value: 'git', label: 'From git' },
        ]}
      />
      {mode === 'upload' ? <Uploader bot={bot} onDone={onDone} /> : <GitForm bot={bot} last={last} onDone={onDone} />}
    </div>
  )
}

function GitForm({ bot, last, onDone }: { bot: Bot; last?: Deploy; onDone: () => void }) {
  const [url, setUrl] = useState(last?.gitUrl ?? '')
  const [ref, setRef] = useState(last?.gitRef ?? 'main')
  const [token, setToken] = useState('')
  const deploy = useMutation({
    mutationFn: () => api.gitDeploy(bot.id, { gitUrl: url, gitRef: ref, token: token || undefined }),
    onSuccess: (d) => {
      toast.success(`Cloned ${d.gitRef} @ ${d.gitCommit?.slice(0, 7)}. Installing on the server…`)
      setToken('')
      onDone()
    },
  })
  return (
    <Card className="px-6 py-5">
      <form
        className="space-y-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          deploy.mutate()
        }}
      >
        <Field label="Repository" htmlFor="g-url">
          <Input id="g-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/you/your-bot.git" className="font-mono text-[14px]" />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Branch or tag" htmlFor="g-ref">
            <Input id="g-ref" value={ref} onChange={(e) => setRef(e.target.value)} className="font-mono text-[14px]" />
          </Field>
          <Field label="Access token" htmlFor="g-token" hint="Only for private repos. Used once, never stored.">
            <Input id="g-token" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} placeholder="optional" />
          </Field>
        </div>
        <FormError error={deploy.error} />
        <Button type="submit" variant="brand" disabled={deploy.isPending || !url}>
          {deploy.isPending ? (
            <>
              <Loader2 className="animate-spin" /> Cloning…
            </>
          ) : (
            'Deploy from git'
          )}
        </Button>
      </form>
    </Card>
  )
}

function Uploader({ bot, onDone }: { bot: Bot; onDone: () => void }) {
  const input = useRef<HTMLInputElement>(null)
  const [drag, setDrag] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  // XHR rather than fetch for upload progress.
  const send = (file: File) => {
    setError(null)
    if (!/\.(zip|tar\.gz|tgz)$/i.test(file.name)) {
      setError('Upload a .zip or .tar.gz of your bot’s folder.')
      return
    }
    const fd = new FormData()
    fd.append('file', file)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', `/api/v1/bots/${bot.id}/deploys`)
    xhr.upload.onprogress = (e) => e.lengthComputable && setProgress(e.loaded / e.total)
    xhr.onload = () => {
      setProgress(null)
      if (xhr.status === 201) {
        toast.success('Uploaded. Installing on the server…')
        onDone()
      } else {
        try {
          setError(JSON.parse(xhr.responseText).error.message)
        } catch {
          setError(`Upload failed (${xhr.status}).`)
        }
      }
    }
    xhr.onerror = () => {
      setProgress(null)
      setError('Upload failed. Check your connection.')
    }
    setProgress(0)
    xhr.send(fd)
  }

  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    setDrag(false)
    const f = e.dataTransfer.files[0]
    if (f) send(f)
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => input.current?.click()}
        onDragOver={(e) => (e.preventDefault(), setDrag(true))}
        onDragLeave={() => setDrag(false)}
        onDrop={onDrop}
        disabled={progress !== null}
        className={cn(
          'flex w-full flex-col items-center justify-center rounded-[20px] border-2 border-dashed px-6 py-12 text-center transition-colors',
          drag ? 'border-brand bg-brand-soft' : 'border-foreground/15 bg-surface hover:border-foreground/30',
        )}
      >
        {progress !== null ? (
          <>
            <Loader2 className="size-8 animate-spin text-brand-text" />
            <p className="mt-4 font-semibold">Uploading {Math.round(progress * 100)}%</p>
            <div className="mt-3 h-1.5 w-48 overflow-hidden rounded-full bg-surface-2">
              <div className="h-full rounded-full bg-brand transition-[width]" style={{ width: `${progress * 100}%` }} />
            </div>
          </>
        ) : (
          <>
            <span className="flex size-14 items-center justify-center rounded-[16px] bg-brand-soft text-brand-text">
              <CloudUpload className="size-6" strokeWidth={2.25} />
            </span>
            <p className="mt-4 font-display text-[18px] font-bold">Drop your bot’s code here</p>
            <p className="mt-1 max-w-[36ch] text-[14px] text-muted-foreground">
              A .zip or .tar.gz of the folder, up to {mb(bot.limits.diskMb)}. Leave out node_modules or .venv; they install on the server.
            </p>
          </>
        )}
      </button>
      <input ref={input} type="file" accept=".zip,.tar.gz,.tgz,application/zip,application/gzip" hidden onChange={(e) => e.target.files?.[0] && send(e.target.files[0])} />
      {error && <p className="mt-3 rounded-[12px] bg-danger-soft px-3.5 py-2.5 text-[14px] font-medium text-danger">{error}</p>}
    </div>
  )
}

function DeployLog({ botId, deployId, onClose }: { botId: string; deployId: string | null; onClose: () => void }) {
  const d = useQuery({ queryKey: ['deploy', botId, deployId], queryFn: () => api.deploy(botId, deployId!), enabled: !!deployId })
  return (
    <Dialog open={!!deployId} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-[760px]">
        <DialogHeader>
          <DialogTitle className="font-display text-[22px] font-bold">Deploy #{d.data?.number} build log</DialogTitle>
        </DialogHeader>
        <pre className="max-h-[60svh] overflow-auto rounded-[14px] bg-[oklch(0.1_0.004_285)] px-4 py-3.5 font-mono text-[12px] leading-relaxed whitespace-pre-wrap text-[oklch(0.88_0.008_90)]">
          {d.isPending ? 'Loading…' : d.data?.log || 'No output was recorded.'}
          {d.data?.error && `\n\n${d.data.error}`}
        </pre>
      </DialogContent>
    </Dialog>
  )
}

// ---------- Metrics ----------

function Metrics({ bot }: { bot: Bot }) {
  const [range, setRange] = useState<'1h' | '24h' | '7d'>('1h')
  const m = useQuery({ queryKey: ['metrics', bot.id, range], queryFn: () => api.metrics(bot.id, range), refetchInterval: 60000, placeholderData: (prev) => prev })
  const pts = m.data ?? []
  return (
    <div className="space-y-5">
      <Segmented
        value={range}
        onChange={setRange}
        className={cn(m.isFetching && 'opacity-80')}
        options={[
          { value: '1h', label: 'Hour' },
          { value: '24h', label: 'Day' },
          { value: '7d', label: 'Week' },
        ]}
      />
      <div className="grid gap-5 lg:grid-cols-2">
        <Card className="px-6 pt-5 pb-4">
          <div className="mb-4 flex items-start justify-between gap-3">
            <div>
              <p className="font-display text-[17px] font-bold">Memory</p>
              <p className="mt-0.5 text-[12.5px] text-faint-foreground">The dashed line is the bot's limit; past it the kernel stops the bot</p>
            </div>
            {pts.length > 0 && <p className="font-display text-[22px] font-bold tabular-nums">{bytes(pts[pts.length - 1].memory)}</p>}
          </div>
          <TimeChart label="Memory" bytes points={pts.map((p) => ({ t: p.t, v: p.memory }))} limit={{ value: bot.limits.memoryMb * 2 ** 20, label: `limit ${mb(bot.limits.memoryMb)}` }} format={(v) => (v === 0 ? '0' : bytes(v))} />
        </Card>
        <Card className="px-6 pt-5 pb-4">
          <div className="mb-4 flex items-start justify-between gap-3">
            <div>
              <p className="font-display text-[17px] font-bold">CPU</p>
              <p className="mt-0.5 text-[12.5px] text-faint-foreground">Percent of the bot's {cores(bot.limits.cpuMillicores)}</p>
            </div>
            {pts.length > 0 && <p className="font-display text-[22px] font-bold tabular-nums">{pct(pts[pts.length - 1].cpu)}</p>}
          </div>
          <TimeChart label="CPU" points={pts.map((p) => ({ t: p.t, v: p.cpu }))} max={100} format={pct} />
        </Card>
      </div>
    </div>
  )
}

// ---------- Environment ----------

function Environment({ bot }: { bot: Bot }) {
  const qc = useQueryClient()
  const env = useQuery({ queryKey: ['env', bot.id], queryFn: () => api.env(bot.id) })
  const [rows, setRows] = useState<(EnvVar & { existing?: boolean })[] | null>(null)
  const [reveal, setReveal] = useState<Record<number, boolean>>({})
  useEffect(() => {
    if (env.data && rows === null) setRows(env.data.map((v) => ({ ...v, existing: v.secret })))
  }, [env.data, rows])
  const save = useMutation({
    mutationFn: () => api.putEnv(bot.id, (rows ?? []).filter((r) => r.key.trim()).map(({ key, value, secret }) => ({ key: key.trim(), value, secret }))),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['env', bot.id] })
      setRows(null)
      toast.success('Saved. The bot restarts with the new values.')
    },
  })
  const update = (i: number, patch: Partial<EnvVar>) => setRows((r) => r!.map((row, j) => (j === i ? { ...row, ...patch } : row)))

  if (!rows) return <Skeleton className="h-64 rounded-[20px]" />
  return (
    <Card className="px-6 py-5">
      <div className="mb-4 flex items-start justify-between gap-4">
        <div>
          <p className="font-display text-[17px] font-bold">Environment variables</p>
          <p className="mt-1 text-[13.5px] text-muted-foreground">Encrypted at rest. Secret values cannot be read back; leave one empty to keep it.</p>
        </div>
      </div>
      <div className="space-y-2.5">
        {rows.map((r, i) => (
          <div key={i} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_auto_auto] items-center gap-2">
            <Input value={r.key} onChange={(e) => update(i, { key: e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, '_') })} placeholder="NAME" className="font-mono text-[13.5px]" />
            <div className="relative">
              <Input
                type={r.secret && !reveal[i] ? 'password' : 'text'}
                value={r.value}
                onChange={(e) => update(i, { value: e.target.value })}
                placeholder={r.secret && r.existing ? '•••••••• unchanged' : 'value'}
                autoComplete="off"
                className="pr-10 font-mono text-[13.5px]"
              />
              {r.secret && (
                <button
                  type="button"
                  onClick={() => setReveal((s) => ({ ...s, [i]: !s[i] }))}
                  className="absolute top-1/2 right-2 -translate-y-1/2 text-faint-foreground hover:text-foreground"
                  aria-label="Show value"
                >
                  {reveal[i] ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              )}
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={() => update(i, { secret: !r.secret })}
              aria-label={r.secret ? 'Make plain' : 'Make secret'}
              className={r.secret ? 'text-brand-text' : ''}
            >
              {r.secret ? <Lock className="size-4" /> : <Unlock className="size-4" />}
            </Button>
            <Button type="button" variant="ghost" size="icon" onClick={() => setRows((rs) => rs!.filter((_, j) => j !== i))} aria-label="Remove">
              <Trash2 className="size-4" />
            </Button>
          </div>
        ))}
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <Button variant="outline" size="sm" onClick={() => setRows((r) => [...r!, { key: '', value: '', secret: false }])}>
          <Plus className="size-4" /> Add variable
        </Button>
        <div className="flex gap-2">
          <Button variant="ghost" size="sm" onClick={() => setRows(null)}>
            Reset
          </Button>
          <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
            Save and restart
          </Button>
        </div>
      </div>
      <div className="mt-3">
        <FormError error={save.error} />
      </div>
    </Card>
  )
}

// ---------- Settings ----------

function Settings({ bot }: { bot: Bot }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState(bot.name)
  const [mem, setMem] = useState(bot.limits.memoryMb)
  const [cpu, setCpu] = useState(bot.limits.cpuMillicores / 1000)
  const [disk, setDisk] = useState(bot.limits.diskMb)
  const [confirm, setConfirm] = useState('')
  const save = useMutation({
    mutationFn: () => api.updateBot(bot.id, { name, memoryMb: mem, cpuMillicores: Math.round(cpu * 1000), diskMb: disk }),
    onSuccess: (b) => {
      qc.setQueryData(['bot', bot.id], b)
      toast.success('Saved')
    },
  })
  const del = useMutation({
    mutationFn: () => api.deleteBot(bot.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['bots'] })
      toast(`${bot.name} was deleted`)
      navigate('/bots')
    },
    onError: (e) => toast.error(e.message),
  })
  return (
    <div className="grid gap-5 lg:grid-cols-2">
      <Card className="px-6 py-5">
        <form
          className="space-y-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <p className="font-display text-[17px] font-bold">Name and size</p>
          <Field label="Name" htmlFor="s-name">
            <Input id="s-name" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Memory (MB)" htmlFor="s-mem">
              <Input id="s-mem" type="number" min={64} step={64} value={mem} onChange={(e) => setMem(Number(e.target.value))} />
            </Field>
            <Field label="CPU (cores)" htmlFor="s-cpu">
              <Input id="s-cpu" type="number" min={0.05} step={0.05} value={cpu} onChange={(e) => setCpu(Number(e.target.value))} />
            </Field>
            <Field label="Disk (MB)" htmlFor="s-disk" hint="Grows, never shrinks.">
              <Input id="s-disk" type="number" min={bot.limits.diskMb} step={128} value={disk} onChange={(e) => setDisk(Number(e.target.value))} />
            </Field>
          </div>
          <FormError error={save.error} />
          <Button type="submit" disabled={save.isPending}>
            Save
          </Button>
        </form>
      </Card>
      <Card className="border-danger/30 px-6 py-5">
        <p className="font-display text-[17px] font-bold text-danger">Delete this bot</p>
        <p className="mt-2 text-[14px] text-muted-foreground">
          Stops it, deletes its container, its disk with everything in <code className="font-mono text-[13px]">/data</code>, and its deploy history. This cannot be undone.
        </p>
        <Field label={`Type ${bot.name} to confirm`} htmlFor="s-del" className="mt-4">
          <Input id="s-del" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="off" />
        </Field>
        <Button variant="destructive" className="mt-4" disabled={confirm !== bot.name || del.isPending} onClick={() => del.mutate()}>
          <Trash2 className="size-4" /> Delete bot
        </Button>
      </Card>
    </div>
  )
}
