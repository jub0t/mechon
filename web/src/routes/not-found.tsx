import { ArrowLeft } from 'lucide-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'

export function NotFoundPage() {
  return (
    <div className="flex flex-col items-center py-24 text-center">
      <p className="text-display text-[clamp(5rem,4rem+5vw,8rem)] text-brand">404</p>
      <p className="mt-4 font-display text-[22px] font-bold">Nothing lives here.</p>
      <p className="mt-2 text-[15px] text-muted-foreground">The page you asked for does not exist, or you do not have access to it.</p>
      <Button asChild className="mt-8">
        <Link to="/">
          <ArrowLeft strokeWidth={2.25} /> Back to the panel
        </Link>
      </Button>
    </div>
  )
}
