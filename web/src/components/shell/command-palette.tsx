import { LogOut, Moon, Sun } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from '@/components/ui/command'
import type { User } from '@/lib/api'
import { navFor } from '@/lib/nav'
import { toggleTheme, useIsDark } from '@/lib/theme'
import { useSignOut } from './user-menu'

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

export function useCommandPalette() {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  return [open, setOpen] as const
}

export function CommandPalette({ user, open, onOpenChange }: { user: User; open: boolean; onOpenChange: (o: boolean) => void }) {
  const navigate = useNavigate()
  const dark = useIsDark()
  const signOut = useSignOut()
  const run = (fn: () => void) => () => {
    onOpenChange(false)
    fn()
  }
  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      showCloseButton={false}
      className="glass top-[18%] translate-y-0 rounded-[22px] border-0 sm:max-w-[560px] [&_[cmdk-group-heading]]:text-[11.5px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:tracking-[0.08em] [&_[cmdk-group-heading]]:text-faint-foreground [&_[cmdk-group-heading]]:uppercase [&_[cmdk-item]]:rounded-[12px] [&_[cmdk-item][data-selected=true]]:bg-foreground/[0.07]"
    >
      <CommandInput placeholder="Jump to a page or run a command…" />
      <CommandList className="max-h-[360px] pb-2">
        <CommandEmpty>Nothing matches that.</CommandEmpty>
        <CommandGroup heading="Go to">
          {navFor(user.role).map((item) => (
            <CommandItem key={item.href} value={`go ${item.label}`} onSelect={run(() => navigate(item.href))}>
              <item.icon strokeWidth={2.25} />
              {item.label}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading="Actions">
          <CommandItem value="toggle theme dark light" onSelect={run(toggleTheme)}>
            {dark ? <Sun /> : <Moon />}
            Switch to {dark ? 'light' : 'dark'} theme
          </CommandItem>
          <CommandItem value="sign out log out" onSelect={run(signOut)}>
            <LogOut />
            Sign out
          </CommandItem>
        </CommandGroup>
      </CommandList>
      <div className="flex items-center justify-between border-t px-4 py-2.5 text-[12px] text-faint-foreground">
        <span>
          <Kbd>↑</Kbd> <Kbd>↓</Kbd> to move · <Kbd>↵</Kbd> to open
        </span>
        <CommandShortcut className="tracking-normal">
          <Kbd>esc</Kbd>
        </CommandShortcut>
      </div>
    </CommandDialog>
  )
}

export function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex h-5 min-w-5 items-center justify-center rounded-[6px] border bg-surface-2 px-1 font-sans text-[11px] font-medium text-muted-foreground">
      {children}
    </kbd>
  )
}
