import { useEffect, useRef } from 'react'
import { cn } from '@/lib/utils'

// A field of small rounded slats that swell with a slow travelling wave and lean toward the cursor.
// Canvas 2D, so it runs everywhere without WebGL. Pauses when the tab is hidden, and draws a single
// still frame when the user prefers reduced motion.

const SLAT_W = 5
const SLAT_H = 18
const STEP_X = 13
const STEP_Y = 27
const CURSOR_RADIUS = 170
const INTRO_MS = 1400

type Rgb = [number, number, number]
const VIOLET: Rgb = [139, 79, 232]
const GLINT: Rgb = [233, 213, 255]

const mix = (a: Rgb, b: Rgb, t: number) => a.map((v, i) => Math.round(v + (b[i] - v) * t)) as Rgb

export function SlatField({ className }: { className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = ref.current
    const ctx = canvas?.getContext('2d')
    if (!canvas || !ctx) return

    const still = matchMedia('(prefers-reduced-motion: reduce)').matches
    let w = 0
    let h = 0
    let raf = 0
    const start = performance.now()
    // Pointer target and eased position, in canvas pixels. Off-canvas until the mouse moves.
    const target = { x: -1e4, y: -1e4 }
    const eased = { x: -1e4, y: -1e4 }

    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      const rect = canvas.getBoundingClientRect()
      w = rect.width
      h = rect.height
      canvas.width = Math.round(w * dpr)
      canvas.height = Math.round(h * dpr)
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      if (still) draw(start + INTRO_MS)
    }

    const draw = (now: number) => {
      const t = now - start
      const intro = Math.min(1, t / INTRO_MS)
      const dark = document.documentElement.classList.contains('dark')
      eased.x += (target.x - eased.x) * 0.08
      eased.y += (target.y - eased.y) * 0.08

      ctx.clearRect(0, 0, w, h)
      const cols = Math.ceil(w / STEP_X) + 1
      const rows = Math.ceil(h / STEP_Y) + 1
      for (let r = 0; r < rows; r++) {
        // Alternate rows are offset by half a step, like brickwork.
        const ox = r % 2 ? STEP_X / 2 : 0
        const y = r * STEP_Y
        // Rows rise in from the bottom during the intro.
        const rowIn = Math.max(0, Math.min(1, intro * 1.6 - (1 - y / h) * 0.6))
        if (rowIn <= 0) continue
        for (let c = 0; c < cols; c++) {
          const x = c * STEP_X + ox
          const wave = Math.sin(x * 0.011 + y * 0.006 - t * 0.0011) * Math.cos(y * 0.009 - x * 0.003 + t * 0.0006)
          const level = (wave + 1) / 2
          const dx = x - eased.x
          const dy = y - eased.y
          const d2 = dx * dx + dy * dy
          const pull = Math.exp(-d2 / (2 * CURSOR_RADIUS * CURSOR_RADIUS))

          const scale = (0.35 + 0.65 * level * level + 0.55 * pull) * rowIn
          const sh = SLAT_H * Math.min(scale, 1.35)
          if (sh < 1.5) continue
          const lean = pull * 5 * Math.sign(dx || 1) * -Math.min(1, Math.abs(dx) / 60)
          const glint = Math.min(1, Math.max(0, (level - 0.82) * 4) + pull * 0.9)
          const [cr, cg, cb] = mix(VIOLET, GLINT, glint)
          const alpha = (dark ? 0.1 + 0.55 * level ** 3 + 0.6 * pull : 0.06 + 0.32 * level ** 3 + 0.4 * pull) * rowIn

          ctx.fillStyle = `rgba(${cr},${cg},${cb},${Math.min(alpha, 0.95)})`
          ctx.beginPath()
          ctx.roundRect(x - SLAT_W / 2 + lean, y - sh / 2, SLAT_W, sh, SLAT_W / 2)
          ctx.fill()
        }
      }
    }

    const loop = (now: number) => {
      draw(now)
      raf = requestAnimationFrame(loop)
    }

    const onMove = (e: PointerEvent) => {
      const rect = canvas.getBoundingClientRect()
      target.x = e.clientX - rect.left
      target.y = e.clientY - rect.top
    }
    const onLeave = () => {
      target.x = target.y = -1e4
    }
    const onVisibility = () => {
      cancelAnimationFrame(raf)
      if (!document.hidden && !still) raf = requestAnimationFrame(loop)
    }

    const ro = new ResizeObserver(resize)
    ro.observe(canvas)
    resize()
    if (!still) {
      raf = requestAnimationFrame(loop)
      window.addEventListener('pointermove', onMove, { passive: true })
      document.documentElement.addEventListener('pointerleave', onLeave)
      document.addEventListener('visibilitychange', onVisibility)
    }
    return () => {
      cancelAnimationFrame(raf)
      ro.disconnect()
      window.removeEventListener('pointermove', onMove)
      document.documentElement.removeEventListener('pointerleave', onLeave)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [])

  return <canvas ref={ref} aria-hidden className={cn('pointer-events-none absolute inset-0 size-full', className)} />
}
