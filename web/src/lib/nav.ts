import {
  Bot,
  Gauge,
  KeyRound,
  Layers,
  type LucideIcon,
  Server,
  Settings,
  Users,
  Webhook,
} from 'lucide-react'
import type { Role } from './api'

export type NavItem = { href: string; label: string; icon: LucideIcon }

const ADMIN: NavItem[] = [
  { href: '/', label: 'Overview', icon: Gauge },
  { href: '/bots', label: 'Bots', icon: Bot },
  { href: '/nodes', label: 'Nodes', icon: Server },
  { href: '/plans', label: 'Plans', icon: Layers },
  { href: '/users', label: 'Users', icon: Users },
  { href: '/webhooks', label: 'Webhooks', icon: Webhook },
  { href: '/settings', label: 'Settings', icon: Settings },
]

const USER: NavItem[] = [
  { href: '/', label: 'Your bots', icon: Bot },
  { href: '/account', label: 'Account', icon: KeyRound },
]

export const navFor = (role: Role) => (role === 'admin' ? ADMIN : USER)

export const isActive = (href: string, pathname: string) =>
  href === '/' ? pathname === '/' : pathname === href || pathname.startsWith(`${href}/`)

/** For users, a bot page belongs to "Your bots" at /. */
export const activeHref = (items: NavItem[], pathname: string) => {
  const hit = items.filter((i) => isActive(i.href, pathname)).sort((a, b) => b.href.length - a.href.length)[0]
  if (hit) return hit.href
  return pathname.startsWith('/bots') ? '/' : null
}
