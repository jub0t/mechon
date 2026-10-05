import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { markdown } from '@codemirror/lang-markdown'
import { python } from '@codemirror/lang-python'
import { yaml } from '@codemirror/lang-yaml'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { EditorView, keymap } from '@codemirror/view'
import { tags as t } from '@lezer/highlight'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import CodeMirror from '@uiw/react-codemirror'
import {
  ChevronRight,
  File,
  FilePlus,
  Folder,
  FolderOpen,
  FolderPlus,
  HardDrive,
  Link2,
  Loader2,
  Maximize2,
  Minimize2,
  RefreshCw,
  Save,
  Trash2,
} from 'lucide-react'
import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { api, ApiError, type Bot, type FileEntry } from '@/lib/api'
import { ago, bytes } from '@/lib/format'
import { cn } from '@/lib/utils'

// Browse and edit a bot's files. The tree loads a folder when it is opened; the editor holds one
// file at a time and warns before unsaved changes are thrown away.

const ROOTS: { path: 'app' | 'data'; label: string; where: string }[] = [
  { path: 'app', label: 'Code', where: '/home/container' },
  { path: 'data', label: 'Data', where: '/data' },
]

const join = (dir: string, name: string) => `${dir}/${name}`
const parentOf = (p: string) => p.slice(0, p.lastIndexOf('/'))

function languageFor(path: string) {
  const ext = path.split('.').pop()?.toLowerCase()
  switch (ext) {
    case 'js':
    case 'mjs':
    case 'cjs':
      return [javascript()]
    case 'jsx':
      return [javascript({ jsx: true })]
    case 'ts':
    case 'mts':
      return [javascript({ typescript: true })]
    case 'tsx':
      return [javascript({ typescript: true, jsx: true })]
    case 'py':
      return [python()]
    case 'json':
      return [json()]
    case 'md':
      return [markdown()]
    case 'yml':
    case 'yaml':
      return [yaml()]
    default:
      return []
  }
}

// Theme from the panel's tokens, so the editor follows light and dark like everything else.
const editorTheme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'transparent', color: 'var(--foreground)', fontSize: '13px' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.65' },
  '.cm-content': { padding: '12px 0', caretColor: 'var(--brand)' },
  '.cm-gutters': { backgroundColor: 'transparent', color: 'var(--faint-foreground)', border: 'none', paddingLeft: '8px' },
  '.cm-activeLine': { backgroundColor: 'color-mix(in oklab, var(--foreground) 4%, transparent)' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--muted-foreground)' },
  '.cm-cursor': { borderLeftColor: 'var(--brand)', borderLeftWidth: '2px' },
  '&.cm-focused': { outline: 'none' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'var(--brand-soft) !important' },
  '.cm-matchingBracket': { backgroundColor: 'var(--brand-soft)', outline: 'none' },
  '.cm-tooltip': { backgroundColor: 'var(--popover)', border: '1px solid var(--border)', borderRadius: '10px' },
})

const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.controlKeyword, t.moduleKeyword, t.operatorKeyword], color: 'var(--brand-text)' },
  { tag: [t.string, t.special(t.string), t.regexp], color: 'var(--success)' },
  { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--orange-text)' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--faint-foreground)', fontStyle: 'italic' },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: 'var(--foreground)', fontWeight: '600' },
  { tag: [t.typeName, t.className, t.namespace], color: 'var(--orange-text)' },
  { tag: [t.propertyName, t.attributeName], color: 'var(--muted-foreground)' },
  { tag: [t.heading], color: 'var(--foreground)', fontWeight: '700' },
  { tag: [t.link, t.url], color: 'var(--brand-text)', textDecoration: 'underline' },
])

export function FileManager({ bot }: { bot: Bot }) {
  const qc = useQueryClient()
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(['app']))
  const [selected, setSelected] = useState<string | null>(null)
  const [focusDir, setFocusDir] = useState('app')
  const [value, setValue] = useState('')
  const [saved, setSaved] = useState('')
  const [full, setFull] = useState(false)
  const [pending, setPending] = useState<string | null>(null)
  const [creating, setCreating] = useState<{ kind: 'file' | 'dir'; dir: string } | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)
  const dirty = selected != null && value !== saved

  const file = useQuery({
    queryKey: ['file', bot.id, selected],
    queryFn: () => api.readFile(bot.id, selected!),
    enabled: !!selected,
    staleTime: Infinity,
    retry: false,
  })
  useEffect(() => {
    if (file.data && !file.data.binary && !file.data.truncated) {
      setValue(file.data.content)
      setSaved(file.data.content)
    }
  }, [file.data])

  const refreshDir = useCallback((dir: string) => qc.invalidateQueries({ queryKey: ['files', bot.id, dir] }), [qc, bot.id])

  const save = useMutation({
    mutationFn: () => api.writeFile(bot.id, selected!, value),
    onSuccess: () => {
      setSaved(value)
      refreshDir(parentOf(selected!))
      toast.success('Saved')
    },
    onError: (e) => toast.error(e.message),
  })
  const remove = useMutation({
    mutationFn: (p: string) => api.deleteFile(bot.id, p),
    onSuccess: (_, p) => {
      refreshDir(parentOf(p))
      if (selected && (selected === p || selected.startsWith(`${p}/`))) {
        setSelected(null)
        setValue('')
        setSaved('')
      }
      setDeleting(null)
      toast(`Deleted ${p.split('/').pop()}`)
    },
    onError: (e) => toast.error(e.message),
  })

  const open = (p: string) => {
    if (p === selected) return
    if (dirty) setPending(p)
    else select(p)
  }
  const select = (p: string) => {
    setSelected(p)
    setFocusDir(parentOf(p))
    setValue('')
    setSaved('')
  }
  const toggleDir = (p: string) => {
    setFocusDir(p)
    setExpanded((s) => {
      const n = new Set(s)
      if (n.has(p)) n.delete(p)
      else n.add(p)
      return n
    })
  }

  const extensions = useMemo(
    () => [
      editorTheme,
      syntaxHighlighting(highlight),
      EditorView.lineWrapping,
      keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => (dirty && !save.isPending && save.mutate(), true) }]),
      ...(selected ? languageFor(selected) : []),
    ],
    [selected, dirty, save],
  )

  useEffect(() => {
    if (!full) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setFull(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [full])

  const inApp = selected?.startsWith('app/') ?? focusDir.startsWith('app')

  return (
    <div className={cn(full && 'fixed inset-0 z-40 bg-background/80 p-3 backdrop-blur-sm sm:p-6')}>
      <div className={cn('grid overflow-hidden rounded-[20px] border bg-surface md:grid-cols-[minmax(220px,280px)_minmax(0,1fr)]', full ? 'h-full' : 'h-[min(640px,72svh)]')}>
        {/* Tree */}
        <div className="flex min-h-0 flex-col border-b md:border-r md:border-b-0">
          <div className="flex items-center gap-1 border-b px-3 py-2">
            <span className="mr-auto pl-1 text-[12px] font-semibold tracking-[0.06em] text-faint-foreground uppercase">Files</span>
            <IconButton label="New file" onClick={() => setCreating({ kind: 'file', dir: focusDir })}>
              <FilePlus className="size-4" />
            </IconButton>
            <IconButton label="New folder" onClick={() => setCreating({ kind: 'dir', dir: focusDir })}>
              <FolderPlus className="size-4" />
            </IconButton>
            <IconButton label="Refresh" onClick={() => qc.invalidateQueries({ queryKey: ['files', bot.id] })}>
              <RefreshCw className="size-4" />
            </IconButton>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2" role="tree" aria-label="Files">
            {ROOTS.map((r) => (
              <div key={r.path}>
                <TreeRow
                  depth={0}
                  icon={<HardDrive className="size-4 shrink-0" />}
                  label={r.label}
                  hint={r.where}
                  open={expanded.has(r.path)}
                  active={focusDir === r.path && !selected}
                  onClick={() => toggleDir(r.path)}
                  chevron
                />
                {expanded.has(r.path) && (
                  <Dir botId={bot.id} path={r.path} depth={1} expanded={expanded} selected={selected} onToggle={toggleDir} onOpen={open} onDelete={setDeleting} />
                )}
              </div>
            ))}
          </div>
        </div>

        {/* Editor */}
        <div className="flex min-h-0 min-w-0 flex-col">
          <div className="flex min-h-[49px] flex-wrap items-center gap-2 border-b px-4 py-2">
            <div className="min-w-0 flex-1">
              {selected ? (
                <p className="flex items-center gap-2 truncate font-mono text-[13px]">
                  <span className="truncate text-muted-foreground">{parentOf(selected)}/</span>
                  <span className="-ml-2 truncate font-semibold text-foreground">{selected.split('/').pop()}</span>
                  {dirty && <span className="size-2 shrink-0 rounded-full bg-orange" title="Unsaved changes" />}
                </p>
              ) : (
                <p className="text-[13px] text-faint-foreground">Pick a file on the left</p>
              )}
              {file.data && (
                <p className="text-[11.5px] text-faint-foreground">
                  {bytes(file.data.size)} · changed {ago(file.data.modTime)}
                </p>
              )}
            </div>
            {selected && (
              <>
                <IconButton label="Delete file" onClick={() => setDeleting(selected)}>
                  <Trash2 className="size-4" />
                </IconButton>
                <Button size="sm" onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
                  {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />} Save
                </Button>
              </>
            )}
            <IconButton label={full ? 'Exit full screen' : 'Full screen'} onClick={() => setFull((f) => !f)}>
              {full ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
            </IconButton>
          </div>
          {inApp && selected && (
            <p className="border-b bg-orange-soft/60 px-4 py-2 text-[12.5px] text-orange-text">
              Files in Code are replaced by your next deploy. Keep anything that must last in Data.
            </p>
          )}
          <div className="min-h-0 flex-1 overflow-hidden">
            {!selected ? (
              <EmptyEditor />
            ) : file.isPending ? (
              <div className="flex h-full items-center justify-center text-faint-foreground">
                <Loader2 className="size-5 animate-spin" />
              </div>
            ) : file.isError ? (
              <Notice title="Could not open this file" text={file.error instanceof ApiError ? file.error.message : 'Try again.'} />
            ) : file.data?.binary ? (
              <Notice title="This is a binary file" text="It cannot be shown as text. Download it by deploying, or delete it here." />
            ) : file.data?.truncated ? (
              <Notice title="Too large to edit here" text="Files up to 1 MB open in the editor. Change larger files with a deploy." />
            ) : (
              <CodeMirror
                value={value}
                onChange={setValue}
                extensions={extensions}
                theme="none"
                height="100%"
                className="h-full"
                basicSetup={{ highlightActiveLine: true, foldGutter: true, autocompletion: false, searchKeymap: true }}
              />
            )}
          </div>
        </div>
      </div>

      <AlertDialog open={pending !== null} onOpenChange={(o) => !o && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="font-display text-[20px] font-bold">Discard your changes?</AlertDialogTitle>
            <AlertDialogDescription>{selected} has edits that are not saved.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="rounded-full">Keep editing</AlertDialogCancel>
            <AlertDialogAction className="rounded-full bg-danger text-white hover:bg-danger/90" onClick={() => (select(pending!), setPending(null))}>
              Discard
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={deleting !== null} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="font-display text-[20px] font-bold">Delete {deleting?.split('/').pop()}?</AlertDialogTitle>
            <AlertDialogDescription>
              <span className="font-mono">{deleting}</span> is removed from the bot's disk. Folders are deleted with everything in them. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="rounded-full">Cancel</AlertDialogCancel>
            <AlertDialogAction className="rounded-full bg-danger text-white hover:bg-danger/90" onClick={(e) => (e.preventDefault(), remove.mutate(deleting!))}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={creating !== null} onOpenChange={(o) => !o && setCreating(null)}>
        <DialogContent className="sm:max-w-[460px]">
          {creating && (
            <NewEntry
              kind={creating.kind}
              dir={creating.dir}
              botId={bot.id}
              onDone={(p) => {
                setCreating(null)
                refreshDir(parentOf(p))
                setExpanded((s) => new Set(s).add(parentOf(p)))
                if (creating.kind === 'file') select(p)
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function Dir({
  botId,
  path,
  depth,
  expanded,
  selected,
  onToggle,
  onOpen,
  onDelete,
}: {
  botId: string
  path: string
  depth: number
  expanded: Set<string>
  selected: string | null
  onToggle: (p: string) => void
  onOpen: (p: string) => void
  onDelete: (p: string) => void
}) {
  const list = useQuery({ queryKey: ['files', botId, path], queryFn: () => api.listFiles(botId, path), retry: false })
  if (list.isPending)
    return (
      <div className="py-1 text-[12.5px] text-faint-foreground" style={{ paddingLeft: 12 + depth * 14 }}>
        Loading…
      </div>
    )
  if (list.isError)
    return (
      <div className="py-1 pr-2 text-[12.5px] text-danger" style={{ paddingLeft: 12 + depth * 14 }}>
        {list.error.message}
      </div>
    )
  if (!list.data.entries.length)
    return (
      <div className="py-1 text-[12.5px] text-faint-foreground" style={{ paddingLeft: 12 + depth * 14 }}>
        Empty
      </div>
    )
  return (
    <>
      {list.data.entries.map((e: FileEntry) => {
        const p = join(path, e.name)
        const open = expanded.has(p)
        return (
          <div key={p}>
            <TreeRow
              depth={depth}
              icon={e.link ? <Link2 className="size-4 shrink-0" /> : e.dir ? open ? <FolderOpen className="size-4 shrink-0" /> : <Folder className="size-4 shrink-0" /> : <File className="size-4 shrink-0" />}
              label={e.name}
              hint={!e.dir && !e.link ? bytes(e.size) : e.link ? 'link' : undefined}
              open={open}
              chevron={e.dir}
              active={selected === p}
              muted={e.link}
              onClick={() => (e.dir ? onToggle(p) : !e.link && onOpen(p))}
              onDelete={() => onDelete(p)}
            />
            {e.dir && open && <Dir botId={botId} path={p} depth={depth + 1} expanded={expanded} selected={selected} onToggle={onToggle} onOpen={onOpen} onDelete={onDelete} />}
          </div>
        )
      })}
    </>
  )
}

function TreeRow({
  depth,
  icon,
  label,
  hint,
  open,
  chevron,
  active,
  muted,
  onClick,
  onDelete,
}: {
  depth: number
  icon: ReactNode
  label: string
  hint?: string
  open?: boolean
  chevron?: boolean
  active?: boolean
  muted?: boolean
  onClick: () => void
  onDelete?: () => void
}) {
  return (
    <div
      role="treeitem"
      aria-expanded={chevron ? open : undefined}
      aria-selected={active}
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), onClick())}
      className={cn(
        'group flex h-8 cursor-pointer items-center gap-1.5 rounded-[9px] pr-1.5 text-[13.5px] transition-colors outline-none',
        active ? 'bg-foreground/[0.08] font-semibold text-foreground' : 'text-muted-foreground hover:bg-surface-2 hover:text-foreground focus-visible:bg-surface-2',
        muted && 'opacity-60',
      )}
      style={{ paddingLeft: 6 + depth * 14 }}
    >
      <ChevronRight className={cn('size-3.5 shrink-0 transition-transform', !chevron && 'invisible', open && 'rotate-90')} />
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {hint && <span className="shrink-0 font-mono text-[11px] text-faint-foreground group-hover:hidden">{hint}</span>}
      {onDelete && (
        <button
          type="button"
          aria-label={`Delete ${label}`}
          onClick={(e) => (e.stopPropagation(), onDelete())}
          className="hidden size-6 shrink-0 items-center justify-center rounded-[7px] text-faint-foreground hover:bg-danger-soft hover:text-danger group-hover:inline-flex"
        >
          <Trash2 className="size-3.5" />
        </button>
      )}
    </div>
  )
}

function NewEntry({ kind, dir, botId, onDone }: { kind: 'file' | 'dir'; dir: string; botId: string; onDone: (path: string) => void }) {
  const [name, setName] = useState('')
  const path = `${dir}/${name.trim().replace(/^\/+|\/+$/g, '')}`
  const create = useMutation({
    mutationFn: () => (kind === 'dir' ? api.makeDir(botId, path) : api.writeFile(botId, path, '')),
    onSuccess: () => onDone(path),
  })
  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (name.trim()) create.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="font-display text-[20px] font-bold">{kind === 'dir' ? 'New folder' : 'New file'}</DialogTitle>
        <DialogDescription>
          In <span className="font-mono">{dir}</span>
        </DialogDescription>
      </DialogHeader>
      <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === 'dir' ? 'commands' : 'config.json'} className="font-mono text-[14px]" />
      {create.error && <p className="text-[13px] text-danger">{create.error.message}</p>}
      <DialogFooter>
        <Button type="submit" disabled={!name.trim() || create.isPending}>
          Create
        </Button>
      </DialogFooter>
    </form>
  )
}

function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="inline-flex size-8 items-center justify-center rounded-[9px] text-muted-foreground transition-colors hover:bg-surface-2 hover:text-foreground"
    >
      {children}
    </button>
  )
}

function EmptyEditor() {
  return (
    <div className="flex h-full flex-col items-center justify-center px-6 text-center">
      <span className="flex size-12 items-center justify-center rounded-[14px] bg-brand-soft text-brand-text">
        <File className="size-5" />
      </span>
      <p className="mt-4 font-display text-[17px] font-bold">Nothing open</p>
      <p className="mt-1.5 max-w-[42ch] text-[13.5px] text-muted-foreground">
        Code is what you deployed. Data is the bot's own storage at <span className="font-mono">/data</span> and survives deploys.
      </p>
    </div>
  )
}

function Notice({ title, text }: { title: string; text: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center px-6 text-center">
      <p className="font-display text-[17px] font-bold">{title}</p>
      <p className="mt-1.5 max-w-[44ch] text-[13.5px] text-muted-foreground">{text}</p>
    </div>
  )
}
