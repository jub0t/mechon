// Formatting for sizes, CPU, durations and times. Kept in one place so numbers read the same everywhere.

export function mb(n: number) {
  if (n >= 1024) {
    const gb = n / 1024
    return `${Number.isInteger(gb) ? gb : gb.toFixed(1)} GB`
  }
  return `${n} MB`
}

export function bytes(n: number) {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function cores(millicores: number) {
  const c = millicores / 1000
  return `${Number.isInteger(c) ? c : c.toFixed(2).replace(/0$/, '')} ${c === 1 ? 'core' : 'cores'}`
}

export function pct(n: number) {
  return `${n >= 10 ? Math.round(n) : n.toFixed(1)}%`
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

export function ago(iso: string | number | null | undefined) {
  if (iso == null) return 'never'
  const s = Math.round((new Date(iso).getTime() - Date.now()) / 1000)
  const abs = Math.abs(s)
  if (abs < 45) return 'just now'
  if (abs < 3600) return rtf.format(Math.round(s / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(s / 3600), 'hour')
  if (abs < 86400 * 30) return rtf.format(Math.round(s / 86400), 'day')
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

export function duration(fromIso: string) {
  const s = Math.max(0, Math.round((Date.now() - new Date(fromIso).getTime()) / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`
}

export function clock(t: number) {
  return new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
}
