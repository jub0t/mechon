import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router'
import { Mark } from '@/components/brand/logo'
import { AppShell } from '@/components/shell/app-shell'
import { Button } from '@/components/ui/button'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ApiError, type User } from '@/lib/api'
import { useMe } from '@/lib/auth'
import { AccountPage } from '@/routes/account'
import { BotPage } from '@/routes/bot'
import { BotsPage } from '@/routes/bots'
import { LoginPage } from '@/routes/login'
import { NodesPage } from '@/routes/nodes'
import { PlansPage } from '@/routes/plans'
import { SettingsPage } from '@/routes/settings'
import { UsersPage } from '@/routes/users'
import { WebhooksPage } from '@/routes/webhooks'
import { NotFoundPage } from '@/routes/not-found'
import { OverviewPage } from '@/routes/overview'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Auth failures are answered by the guard, not retried.
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
      refetchOnWindowFocus: true,
    },
  },
})

function Splash() {
  return (
    <div className="flex min-h-svh items-center justify-center">
      <Mark className="size-11 animate-pulse" />
    </div>
  )
}

function Unreachable({ error, retry }: { error: Error; retry: () => void }) {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center px-6 text-center">
      <Mark className="size-11 opacity-60 grayscale" />
      <p className="mt-6 font-display text-[22px] font-bold">Cannot reach the panel</p>
      <p className="mt-2 max-w-[42ch] text-[15px] text-muted-foreground">{error.message}</p>
      <Button className="mt-6" onClick={retry}>
        Try again
      </Button>
    </div>
  )
}

/** Signed-in area. Sends visitors to /login and remembers where they were going. */
function Protected() {
  const me = useMe()
  const location = useLocation()
  if (me.isPending) return <Splash />
  if (me.isError) return <Unreachable error={me.error} retry={() => me.refetch()} />
  if (!me.data) {
    const next = location.pathname + location.search
    return <Navigate to={next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`} replace />
  }
  return <AppShell user={me.data} />
}

function useUser(): User {
  return useMe().data!
}

function AdminOnly({ children }: { children: ReactNode }) {
  return useUser().role === 'admin' ? children : <NotFoundPage />
}

function Home() {
  const user = useUser()
  return user.role === 'admin' ? <OverviewPage user={user} /> : <BotsPage title="Your bots" />
}

const admin = (page: ReactNode) => <AdminOnly>{page}</AdminOnly>

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route element={<Protected />}>
              <Route index element={<Home />} />
              <Route path="bots" element={<BotsPage />} />
              <Route path="bots/:id" element={<BotPage />} />
              <Route path="nodes" element={admin(<NodesPage />)} />
              <Route path="plans" element={admin(<PlansPage />)} />
              <Route path="users" element={admin(<UsersPage />)} />
              <Route path="webhooks" element={admin(<WebhooksPage />)} />
              <Route path="settings" element={admin(<SettingsPage />)} />
              <Route path="account" element={<AccountPage />} />
              <Route path="*" element={<NotFoundPage />} />
            </Route>
          </Routes>
        </BrowserRouter>
        <Toaster position="bottom-right" />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
)
