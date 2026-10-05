import { useSyncExternalStore } from 'react'

// Whether the desktop sidebar is folded to an icon rail. Remembered per browser.
const KEY = 'mechon-sidebar'
const listeners = new Set<() => void>()

let collapsed = (() => {
  try {
    return localStorage.getItem(KEY) === 'collapsed'
  } catch {
    return false
  }
})()

export function useSidebarCollapsed() {
  return useSyncExternalStore(
    (cb) => (listeners.add(cb), () => listeners.delete(cb)),
    () => collapsed,
    () => false,
  )
}

export function toggleSidebar() {
  collapsed = !collapsed
  try {
    localStorage.setItem(KEY, collapsed ? 'collapsed' : 'open')
  } catch {
    // still toggles for this visit
  }
  listeners.forEach((l) => l())
}
