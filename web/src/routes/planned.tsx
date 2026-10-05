import type { LucideIcon } from 'lucide-react'
import { Card } from '@/components/page/card'
import { PageHeader } from '@/components/page/page-header'
import { Reveal } from '@/components/motion/reveal'

/** Stand-in for a page whose milestone has not been built yet. */
export function PlannedPage({ icon: Icon, title, description, milestone, bullets }: { icon: LucideIcon; title: string; description: string; milestone: number; bullets: string[] }) {
  return (
    <>
      <Reveal>
        <PageHeader title={title} description={description} />
      </Reveal>
      <Reveal delay={0.06}>
        <Card className="grid gap-8 px-6 py-8 sm:px-8 md:grid-cols-[auto_1fr] md:items-start">
          <span className="flex size-14 items-center justify-center rounded-[16px] bg-brand-soft text-brand-text">
            <Icon className="size-6" strokeWidth={2.25} />
          </span>
          <div>
            <span className="inline-flex h-7 items-center rounded-full bg-orange-soft px-2.5 text-[12.5px] font-semibold text-orange-text">
              Arrives in milestone {milestone}
            </span>
            <ul className="mt-5 space-y-3">
              {bullets.map((b) => (
                <li key={b} className="flex gap-3 text-[15px] leading-relaxed text-muted-foreground">
                  <span className="mt-[9px] size-1.5 shrink-0 rounded-full bg-faint-foreground" />
                  {b}
                </li>
              ))}
            </ul>
          </div>
        </Card>
      </Reveal>
    </>
  )
}
