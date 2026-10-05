import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Search } from 'lucide-react'
import { Link, Outlet, useLocation } from 'react-router'
import { Logo } from '@/components/brand/logo'
import type { User } from '@/lib/api'
import { activeHref, navFor } from '@/lib/nav'
import { cn } from '@/lib/utils'
import { CommandPalette, isMac, Kbd, useCommandPalette } from './command-palette'
import { ThemeToggle } from './theme-toggle'
import { UserMenu } from './user-menu'

export function AppShell({ user }: { user: User }) {
  const [paletteOpen, setPaletteOpen] = useCommandPalette()
  const { pathname } = useLocation()
  const reduce = useReducedMotion()
  const items = navFor(user.role)
  const current = activeHref(items, pathname)

  return (
    <div className="flex min-h-svh">
      <aside className="sticky top-0 hidden h-svh w-[264px] shrink-0 flex-col border-r bg-surface px-4 py-5 lg:flex">
        <div className="flex items-center justify-between pl-2">
          <Link to="/" aria-label="Mechon home">
            <Logo />
          </Link>
          <ThemeToggle />
        </div>

        <button
          type="button"
          onClick={() => setPaletteOpen(true)}
          className="mt-6 flex h-10 items-center gap-2.5 rounded-[12px] border bg-background/60 px-3 text-[14px] text-faint-foreground transition-colors hover:border-foreground/20 hover:text-muted-foreground"
        >
          <Search className="size-4" strokeWidth={2.25} />
          <span className="flex-1 text-left">Search</span>
          <span className="flex gap-0.5">
            <Kbd>{isMac ? '⌘' : 'Ctrl'}</Kbd>
            <Kbd>K</Kbd>
          </span>
        </button>

        <p className="mt-6 mb-2 pl-3 text-[11.5px] font-semibold tracking-[0.08em] text-faint-foreground uppercase">
          {user.role === 'admin' ? 'Hosting' : 'Workspace'}
        </p>
        <nav className="flex flex-col gap-0.5">
          {items.map((item) => {
            const active = item.href === current
            return (
              <Link
                key={item.href}
                to={item.href}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'relative flex h-10 items-center gap-3 rounded-[12px] px-3 text-[14.5px] font-medium transition-colors',
                  active ? 'text-background' : 'text-muted-foreground hover:bg-surface-2 hover:text-foreground',
                )}
              >
                {/* The active pill slides between items instead of jumping. */}
                {active && (
                  <motion.span
                    layoutId="nav-pill"
                    className="absolute inset-0 rounded-[12px] bg-foreground"
                    transition={reduce ? { duration: 0 } : { type: 'spring', stiffness: 520, damping: 40 }}
                  />
                )}
                <item.icon className="relative size-[18px] shrink-0" strokeWidth={2.25} aria-hidden />
                <span className="relative">{item.label}</span>
              </Link>
            )
          })}
        </nav>

        <div className="mt-auto">
          <UserMenu user={user} />
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Mobile top bar and scrolling nav */}
        <header className="sticky top-0 z-30 border-b bg-surface/85 backdrop-blur-xl lg:hidden">
          <div className="flex h-14 items-center justify-between px-4">
            <Link to="/" aria-label="Mechon home">
              <Logo />
            </Link>
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => setPaletteOpen(true)}
                aria-label="Search"
                className="inline-flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-surface-2 hover:text-foreground"
              >
                <Search className="size-[18px]" strokeWidth={2} />
              </button>
              <ThemeToggle />
            </div>
          </div>
          <nav className="no-scrollbar flex gap-1 overflow-x-auto px-3 pb-2">
            {items.map((item) => {
              const active = item.href === current
              return (
                <Link
                  key={item.href}
                  to={item.href}
                  aria-current={active ? 'page' : undefined}
                  className={cn(
                    'flex h-9 shrink-0 items-center gap-2 rounded-full px-3.5 text-[14px] font-medium transition-colors',
                    active ? 'bg-foreground text-background' : 'text-muted-foreground hover:bg-surface-2 hover:text-foreground',
                  )}
                >
                  <item.icon className="size-4" strokeWidth={2.25} aria-hidden />
                  {item.label}
                </Link>
              )
            })}
          </nav>
        </header>

        <main className="flex-1 px-4 py-8 sm:px-8 lg:px-12 lg:py-12">
          <AnimatePresence mode="wait" initial={false}>
            <motion.div
              key={pathname}
              className="mx-auto w-full max-w-[1200px]"
              initial={reduce ? false : { opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={reduce ? undefined : { opacity: 0, y: -4 }}
              transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
            >
              <Outlet />
            </motion.div>
          </AnimatePresence>
        </main>
      </div>

      <CommandPalette user={user} open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  )
}
