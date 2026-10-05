import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import type { Bot, BotState, BotUsage, LogLine } from './api'

// Live feed for one bot over Server-Sent Events: state changes, stats, logs and deploy progress.
// EventSource reconnects by itself; the server re-sends a snapshot on every connect.

export type LiveState = { state: BotState; error: string; exitCode: number | null; restarts: number; at: string }
export type DeployEvent = { deployId: string; phase: 'fetching' | 'installing' | 'live' | 'failed'; lines?: LogLine[]; error?: string }
export type StatsPoint = BotUsage & { t: number }

const MAX_LOGS = 2000
const MAX_POINTS = 60

export function useBotStream(botId: string) {
  const qc = useQueryClient()
  const [connected, setConnected] = useState(false)
  const [nodeOnline, setNodeOnline] = useState(true)
  const [live, setLive] = useState<LiveState | null>(null)
  const [logs, setLogs] = useState<LogLine[]>([])
  const [points, setPoints] = useState<StatsPoint[]>([])
  const [deploy, setDeploy] = useState<{ id: string; phase: DeployEvent['phase']; lines: LogLine[]; error?: string } | null>(null)
  const pending = useRef<LogLine[]>([])

  useEffect(() => {
    const es = new EventSource(`/api/v1/bots/${botId}/stream`)
    const on = <T,>(type: string, fn: (d: T) => void) => es.addEventListener(type, (e) => fn(JSON.parse((e as MessageEvent).data)))

    // Batch log lines into one render per animation frame; busy bots print a lot.
    let raf = 0
    const flush = () => {
      raf = 0
      const add = pending.current
      pending.current = []
      setLogs((l) => {
        const next = l.concat(add)
        return next.length > MAX_LOGS ? next.slice(next.length - MAX_LOGS) : next
      })
    }

    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    on<{ bot: Bot; nodeOnline: boolean }>('hello', ({ bot, nodeOnline }) => {
      setNodeOnline(nodeOnline)
      qc.setQueryData(['bot', botId], bot)
    })
    on<LiveState>('state', (s) => {
      setLive(s)
      qc.invalidateQueries({ queryKey: ['bot', botId] })
    })
    on<BotUsage>('stats', (s) => setPoints((p) => [...p.slice(-(MAX_POINTS - 1)), { ...s, t: Date.now() }]))
    on<LogLine[]>('logs.reset', (lines) => {
      pending.current = []
      setLogs(lines.slice(-MAX_LOGS))
    })
    on<LogLine[]>('logs', (lines) => {
      pending.current.push(...lines)
      if (!raf) raf = requestAnimationFrame(flush)
    })
    on<DeployEvent>('deploy', (d) => {
      setDeploy((cur) => ({
        id: d.deployId,
        phase: d.phase,
        error: d.error,
        lines: [...(cur?.id === d.deployId ? cur.lines : []), ...(d.lines ?? [])].slice(-500),
      }))
      if (d.phase === 'live' || d.phase === 'failed') {
        qc.invalidateQueries({ queryKey: ['deploys', botId] })
        qc.invalidateQueries({ queryKey: ['bot', botId] })
      }
    })
    return () => {
      cancelAnimationFrame(raf)
      es.close()
    }
  }, [botId, qc])

  return { connected, nodeOnline, live, logs, setLogs, points, deploy, setDeploy }
}
