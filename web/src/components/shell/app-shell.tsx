import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { Link, Outlet, useLocation } from 'react-router'
import { Logo, Mark } from '@/components/brand/logo'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { User } from '@/lib/api'
import { activeHref, navFor } from '@/lib/nav'
import { toggleSidebar, useSidebarCollapsed } from '@/lib/sidebar'
import { cn } from '@/lib/utils'
import { CommandPalette, isMac, Kbd, useCommandPalette } from './command-palette'
import { ThemeToggle } from './theme-toggle'
import { UserMenu } from './user-menu'

function RailButton({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          aria-label={label}
          className="inline-flex size-10 items-center justify-center rounded-[12px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
        >
          {children}
        </button>
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}

export function AppShell({ user }: { user: User }) {
  const [paletteOpen, setPaletteOpen] = useCommandPalette()
  const collapsed = useSidebarCollapsed()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'b' && (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey) {
        e.preventDefault()
        toggleSidebar()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  const { pathname } = useLocation()
  const reduce = useReducedMotion()
  const items = navFor(user.role)
  const current = activeHref(items, pathname)

  // The window never scrolls: the sidebar is pinned at full height and only <main> scrolls, so
  // there is one scrollbar and no blank overscroll past the end of a page.
  const scroller = useRef<HTMLElement>(null)
  useEffect(() => {
    scroller.current?.scrollTo({ top: 0 })
  }, [pathname])

  return (
    <div className="flex h-svh overflow-hidden">
      <motion.aside
        initial={false}
        animate={{ width: collapsed ? 76 : 264 }}
        transition={reduce ? { duration: 0 } : { type: 'spring', stiffness: 420, damping: 40 }}
        className="hidden h-full shrink-0 flex-col overflow-hidden border-r bg-surface py-5 lg:flex"
        style={{ paddingInline: collapsed ? 14 : 16 }}
      >
        {collapsed ? (
          <div className="flex flex-col items-center gap-2">
            <Link to="/" aria-label="Mechon home">
              <Mark className="size-8" />
            </Link>
            <RailButton label={`Expand sidebar (${isMac ? '⌘' : 'Ctrl'} B)`} onClick={toggleSidebar}>
              <PanelLeftOpen className="size-[18px]" strokeWidth={2} />
            </RailButton>
          </div>
        ) : (
          <div className="flex items-center justify-between pl-2">
            <Link to="/" aria-label="Mechon home">
              <Logo />
            </Link>
            <div className="flex items-center">
              <ThemeToggle />
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="ghost" size="icon" onClick={toggleSidebar} aria-label="Collapse sidebar">
                    <PanelLeftClose strokeWidth={2} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  Collapse <Kbd>{isMac ? '⌘' : 'Ctrl'}</Kbd> <Kbd>B</Kbd>
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        )}

        {collapsed ? (
          <div className="mt-5 flex justify-center">
            <RailButton label="Search" onClick={() => setPaletteOpen(true)}>
              <Search className="size-[18px]" strokeWidth={2.25} />
            </RailButton>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => setPaletteOpen(true)}
            className="mt-6 flex h-10 shrink-0 items-center gap-2.5 rounded-[12px] border bg-background/60 px-3 text-[14px] whitespace-nowrap text-faint-foreground transition-colors hover:border-foreground/20 hover:text-muted-foreground"
          >
            <Search className="size-4 shrink-0" strokeWidth={2.25} />
            <span className="flex-1 text-left">Search</span>
            <span className="flex gap-0.5">
              <Kbd>{isMac ? '⌘' : 'Ctrl'}</Kbd>
              <Kbd>K</Kbd>
            </span>
          </button>
        )}

        <p
          className={cn(
            'mt-6 mb-2 pl-3 text-[11.5px] font-semibold tracking-[0.08em] whitespace-nowrap text-faint-foreground uppercase transition-opacity',
            collapsed && 'pointer-events-none opacity-0',
          )}
        >
          {user.role === 'admin' ? 'Hosting' : 'Workspace'}
        </p>
        <nav className="flex flex-col gap-0.5">
          {items.map((item) => {
            const active = item.href === current
            const link = (
              <Link
                key={item.href}
                to={item.href}
                aria-current={active ? 'page' : undefined}
                aria-label={collapsed ? item.label : undefined}
                className={cn(
                  'relative flex h-10 items-center gap-3 rounded-[12px] text-[14.5px] font-medium whitespace-nowrap transition-colors',
                  collapsed ? 'w-12 justify-center' : 'px-3',
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
                {!collapsed && <span className="relative">{item.label}</span>}
              </Link>
            )
            return collapsed ? (
              <Tooltip key={item.href}>
                <TooltipTrigger asChild>{link}</TooltipTrigger>
                <TooltipContent side="right">{item.label}</TooltipContent>
              </Tooltip>
            ) : (
              link
            )
          })}
        </nav>

        <div className="mt-auto flex flex-col items-stretch gap-2">
          {collapsed && (
            <div className="flex justify-center">
              <ThemeToggle />
            </div>
          )}
          <UserMenu user={user} compact={collapsed} />
        </div>
      </motion.aside>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {/* Mobile top bar and scrolling nav */}
        <header className="z-30 shrink-0 border-b bg-surface lg:hidden">
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

        <main ref={scroller} className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-8 sm:px-8 lg:px-12 lg:py-12">
          <AnimatePresence mode="wait" initial={false}>
            <motion.div
              key={pathname}
              className="mx-auto w-full max-w-[1280px]"
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
