import type { BotState, DeployStatus } from '@/lib/api'
import { cn } from '@/lib/utils'

type Tone = 'success' | 'brand' | 'orange' | 'danger' | 'neutral'

const tones: Record<Tone, string> = {
  success: 'bg-success-soft text-success',
  brand: 'bg-brand-soft text-brand-text',
  orange: 'bg-orange-soft text-orange-text',
  danger: 'bg-danger-soft text-danger',
  neutral: 'bg-surface-2 text-muted-foreground',
}

export function Pill({ tone, children, pulse, className }: { tone: Tone; children: React.ReactNode; pulse?: boolean; className?: string }) {
  return (
    <span className={cn('inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full px-2.5 text-[12.5px] font-semibold whitespace-nowrap', tones[tone], className)}>
      <span className="relative inline-flex size-1.5">
        {pulse && <span className="absolute inset-0 animate-ping rounded-full bg-current opacity-60" />}
        <span className="relative inline-flex size-1.5 rounded-full bg-current" />
      </span>
      {children}
    </span>
  )
}

const botStates: Record<BotState, { tone: Tone; label: string; pulse?: boolean }> = {
  running: { tone: 'success', label: 'Running', pulse: true },
  installing: { tone: 'orange', label: 'Installing', pulse: true },
  pending: { tone: 'neutral', label: 'Waiting for code' },
  stopped: { tone: 'neutral', label: 'Stopped' },
  crashed: { tone: 'danger', label: 'Crashed' },
  unknown: { tone: 'orange', label: 'Node offline' },
}

export function BotStateBadge({ state, className }: { state: BotState; className?: string }) {
  const s = botStates[state] ?? botStates.unknown
  return (
    <Pill tone={s.tone} pulse={s.pulse} className={className}>
      {s.label}
    </Pill>
  )
}

const deployStates: Record<DeployStatus, { tone: Tone; label: string; pulse?: boolean }> = {
  queued: { tone: 'neutral', label: 'Queued', pulse: true },
  fetching: { tone: 'orange', label: 'Fetching', pulse: true },
  installing: { tone: 'orange', label: 'Installing', pulse: true },
  live: { tone: 'success', label: 'Live' },
  failed: { tone: 'danger', label: 'Failed' },
  superseded: { tone: 'neutral', label: 'Replaced' },
}

export function DeployBadge({ status }: { status: DeployStatus }) {
  const s = deployStates[status]
  return (
    <Pill tone={s.tone} pulse={s.pulse}>
      {s.label}
    </Pill>
  )
}
