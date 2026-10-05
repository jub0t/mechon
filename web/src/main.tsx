import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Bot, KeyRound, Layers, Server, Settings, Users, Webhook } from 'lucide-react'
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
import { LoginPage } from '@/routes/login'
import { NotFoundPage } from '@/routes/not-found'
import { OverviewPage } from '@/routes/overview'
import { PlannedPage } from '@/routes/planned'
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

function Overview() {
  return <OverviewPage user={useUser()} />
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
              <Route index element={<Overview />} />
              <Route
                path="bots"
                element={admin(
                  <PlannedPage icon={Bot} milestone={4} title="Bots" description="Every bot on every node, with its owner, state and resource use." bullets={['Start, stop, restart and delete any bot', 'Live console and per-minute metrics', 'Filter by node, owner, template or state']} />,
                )}
              />
              <Route
                path="nodes"
                element={admin(
                  <PlannedPage icon={Server} milestone={3} title="Nodes" description="The servers your bots run on. Each one runs the Mechon agent next to Docker." bullets={['Add a node and copy a one-line install command', 'Live CPU, memory and disk capacity per node', 'Maintenance mode to stop new bots landing on a node']} />,
                )}
              />
              <Route
                path="plans"
                element={admin(
                  <PlannedPage icon={Layers} milestone={2} title="Plans" description="What you sell. A plan is a pool of bots, memory, CPU and disk that a user can split across their bots." bullets={['Limits checked on every change, then enforced by cgroups', 'A hardened tier that runs bots under gVisor', 'Restrict a plan to certain templates']} />,
                )}
              />
              <Route
                path="users"
                element={admin(
                  <PlannedPage icon={Users} milestone={2} title="Users" description="Your customers and fellow admins, and the plans they hold." bullets={['Create users by hand or from your billing system', 'Suspend and unsuspend, which stops and blocks their bots', 'See each user’s usage against their plan']} />,
                )}
              />
              <Route
                path="webhooks"
                element={admin(
                  <PlannedPage icon={Webhook} milestone={6} title="Webhooks" description="Tell your billing system and tools when something happens." bullets={['Signed with HMAC-SHA256, retried for 24 hours', 'Bot crashed, deploy live or failed, node offline, and more', 'Delivery log with request and response']} />,
                )}
              />
              <Route
                path="settings"
                element={admin(
                  <PlannedPage icon={Settings} milestone={6} title="Settings" description="Install-wide settings for your hosting panel." bullets={['Your brand name and logo on the login page', 'Audit log of every admin action', 'Operator API keys for billing integrations']} />,
                )}
              />
              <Route
                path="account"
                element={<PlannedPage icon={KeyRound} milestone={2} title="Account" description="Your profile, password and API keys." bullets={['Change your name, email and password', 'Create scoped API keys for CI deploys', 'Sign out of other sessions']} />}
              />
              <Route path="*" element={<NotFoundPage />} />
            </Route>
          </Routes>
        </BrowserRouter>
        <Toaster position="bottom-right" />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
)
