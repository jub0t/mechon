import { ArrowRight, Eye, EyeOff, Loader2 } from 'lucide-react'
import { motion, useAnimationControls, useReducedMotion } from 'motion/react'
import { type FormEvent, useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router'
import { SlatField } from '@/components/backdrop/slat-field'
import { Logo } from '@/components/brand/logo'
import { Reveal } from '@/components/motion/reveal'
import { ThemeToggle } from '@/components/shell/theme-toggle'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api } from '@/lib/api'
import { useLogin, useMe } from '@/lib/auth'

/** Only same-site paths are followed after sign-in, so ?next= cannot bounce users off-site. */
function safeNext(raw: string | null) {
  return raw && raw.startsWith('/') && !raw.startsWith('//') && !raw.startsWith('/\\') ? raw : '/'
}

export function LoginPage() {
  const me = useMe()
  const site = useQuery({ queryKey: ['public-settings'], queryFn: api.publicSettings, staleTime: 5 * 60_000 })
  const brand = site.data?.brandName || 'Mechon'
  const login = useLogin()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const shake = useAnimationControls()
  const reduce = useReducedMotion()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [reveal, setReveal] = useState(false)
  const [capsLock, setCapsLock] = useState(false)

  if (me.data) return <Navigate to={next} replace />

  const error = login.error instanceof ApiError ? login.error : login.error ? new ApiError(0, 'unknown', 'Something went wrong.') : null

  function submit(e: FormEvent) {
    e.preventDefault()
    login.mutate(
      { email, password },
      {
        onSuccess: () => navigate(next, { replace: true }),
        onError: () => {
          if (!reduce) shake.start({ x: [0, -10, 9, -6, 4, 0], transition: { duration: 0.42 } })
        },
      },
    )
  }

  return (
    <div className="relative flex min-h-svh flex-col overflow-hidden">
      <div className="absolute inset-0 [mask-image:radial-gradient(ellipse_80%_70%_at_50%_45%,black_35%,transparent_85%)]">
        <SlatField />
      </div>

      <header className="relative flex items-center justify-between px-5 py-5 sm:px-8">
        {site.data?.brandName ? <span className="font-display text-[19px] font-bold tracking-[-0.02em]">{site.data.brandName}</span> : <Logo />}
        <ThemeToggle />
      </header>

      <main className="relative flex flex-1 items-center justify-center px-4 pb-16">
        <Reveal y={18} className="w-full max-w-[440px]">
          <motion.div animate={shake} className="glass rounded-[30px] px-6 py-9 sm:px-10 sm:py-11">
            <h1 className="text-display text-[clamp(2.4rem,2rem+1.6vw,3rem)]">Welcome back.</h1>
            <p className="mt-3 text-[15.5px] text-muted-foreground">Sign in to {brand} to run your bots.</p>

            <form onSubmit={submit} className="mt-8 space-y-5" noValidate>
              <div className="space-y-2">
                <Label htmlFor="email" className="text-[13.5px] font-semibold">
                  Email
                </Label>
                <Input
                  id="email"
                  type="email"
                  autoComplete="username"
                  autoFocus
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="you@example.com"
                  aria-invalid={error?.code === 'bad_credentials' || undefined}
                />
              </div>

              <div className="space-y-2">
                <div className="flex items-baseline justify-between">
                  <Label htmlFor="password" className="text-[13.5px] font-semibold">
                    Password
                  </Label>
                  {capsLock && <span className="text-[12.5px] font-medium text-orange-text">Caps Lock is on</span>}
                </div>
                <div className="relative">
                  <Input
                    id="password"
                    type={reveal ? 'text' : 'password'}
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    onKeyUp={(e) => setCapsLock(e.getModifierState('CapsLock'))}
                    className="pr-11"
                    aria-invalid={error?.code === 'bad_credentials' || undefined}
                  />
                  <button
                    type="button"
                    onClick={() => setReveal((r) => !r)}
                    aria-label={reveal ? 'Hide password' : 'Show password'}
                    className="absolute top-1/2 right-1.5 inline-flex size-8 -translate-y-1/2 items-center justify-center rounded-full text-faint-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
                  >
                    {reveal ? <EyeOff className="size-[17px]" /> : <Eye className="size-[17px]" />}
                  </button>
                </div>
              </div>

              <div aria-live="polite" className="min-h-0">
                {error && (
                  <motion.p
                    initial={reduce ? false : { opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: 'auto' }}
                    className="rounded-[12px] bg-danger-soft px-3.5 py-2.5 text-[14px] font-medium text-danger"
                  >
                    {error.message}
                  </motion.p>
                )}
              </div>

              <Button type="submit" size="lg" className="w-full" disabled={login.isPending || !email || !password}>
                {login.isPending ? (
                  <>
                    <Loader2 className="animate-spin" /> Signing in
                  </>
                ) : (
                  <>
                    Sign in <ArrowRight strokeWidth={2.25} />
                  </>
                )}
              </Button>
            </form>
          </motion.div>
          <p className="mt-6 text-center text-[13px] text-faint-foreground">
            Forgot your password?{' '}
            {site.data?.supportUrl ? (
              <a href={site.data.supportUrl} target="_blank" rel="noreferrer" className="font-semibold text-muted-foreground hover:text-foreground">
                Contact support
              </a>
            ) : (
              "Ask your host's admin to reset it."
            )}
          </p>
        </Reveal>
      </main>

      <footer className="relative px-5 pb-6 text-center text-[12.5px] text-faint-foreground">
        Powered by{' '}
        <a href="https://github.com/jub0t/mechon" target="_blank" rel="noreferrer" className="font-semibold text-muted-foreground hover:text-foreground">
          Mechon
        </a>
      </footer>
    </div>
  )
}
