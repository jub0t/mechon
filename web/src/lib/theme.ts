import { useSyncExternalStore } from 'react'

const KEY = 'mechon-theme'

function subscribe(cb: () => void) {
  const obs = new MutationObserver(cb)
  obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  return () => obs.disconnect()
}

const isDark = () => document.documentElement.classList.contains('dark')

export function useIsDark() {
  return useSyncExternalStore(subscribe, isDark, () => true)
}

export function toggleTheme() {
  const next = isDark() ? 'light' : 'dark'
  const apply = () => document.documentElement.classList.toggle('dark', next === 'dark')
  // Cross-fade the whole page where the browser supports view transitions.
  const doc = document as Document & { startViewTransition?: (cb: () => void) => unknown }
  if (doc.startViewTransition && !matchMedia('(prefers-reduced-motion: reduce)').matches) doc.startViewTransition(apply)
  else apply()
  try {
    localStorage.setItem(KEY, next)
  } catch {
    // Private mode or blocked storage: the toggle still works for this page view.
  }
}
