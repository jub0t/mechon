import { ChevronsUpDown, KeyRound, LogOut, Moon, Sun } from 'lucide-react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { User } from '@/lib/api'
import { useLogout } from '@/lib/auth'
import { toggleTheme, useIsDark } from '@/lib/theme'
import { cn } from '@/lib/utils'

export function initials(name: string) {
  const parts = name.trim().split(/\s+/)
  return ((parts[0]?.[0] ?? '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase() || '?'
}

export function useSignOut() {
  const logout = useLogout()
  const navigate = useNavigate()
  return () =>
    logout.mutate(undefined, {
      onSettled: () => {
        navigate('/login', { replace: true })
        toast('Signed out')
      },
    })
}

export function UserMenu({ user, compact = false }: { user: User; compact?: boolean }) {
  const dark = useIsDark()
  const navigate = useNavigate()
  const signOut = useSignOut()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={compact ? `Account menu for ${user.name}` : undefined}
        className={cn(
          'flex items-center gap-3 rounded-[14px] p-2 text-left transition-colors outline-none hover:bg-surface-2 focus-visible:bg-surface-2 data-[state=open]:bg-surface-2',
          compact ? 'mx-auto' : 'w-full',
        )}
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-brand-soft font-display text-[13px] font-bold text-brand-text">
          {initials(user.name)}
        </span>
        {!compact && (
          <>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[14px] font-semibold">{user.name}</span>
              <span className="block truncate text-[12.5px] text-muted-foreground">{user.email}</span>
            </span>
            <ChevronsUpDown className="size-4 shrink-0 text-faint-foreground" />
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent side={compact ? 'right' : 'top'} align={compact ? 'end' : 'start'} className={cn('min-w-56 rounded-[14px] p-1.5', !compact && 'w-[var(--radix-dropdown-menu-trigger-width)]')}>
        <DropdownMenuLabel className="text-[12px] font-semibold tracking-[0.06em] text-faint-foreground uppercase">
          {user.role === 'admin' ? 'Administrator' : 'Account'}
        </DropdownMenuLabel>
        <DropdownMenuItem className="rounded-[10px]" onSelect={() => navigate('/account')}>
          <KeyRound /> Account and API keys
        </DropdownMenuItem>
        <DropdownMenuItem className="rounded-[10px]" onSelect={(e) => (e.preventDefault(), toggleTheme())}>
          {dark ? <Sun /> : <Moon />} {dark ? 'Light theme' : 'Dark theme'}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem className="rounded-[10px]" variant="destructive" onSelect={signOut}>
          <LogOut /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
