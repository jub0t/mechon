import { useEffect } from 'react'
import { useSearchParams } from 'react-router'

/** Opens a page's "create" dialog when the URL carries ?new=1 (used by the setup checklist), then
 *  drops the parameter so a reload or back navigation does not open it again. */
export function useNewParam(open: () => void) {
  const [params, setParams] = useSearchParams()
  const wanted = params.get('new') === '1'
  useEffect(() => {
    if (!wanted) return
    open()
    const next = new URLSearchParams(params)
    next.delete('new')
    setParams(next, { replace: true })
  }, [wanted, open, params, setParams])
}
