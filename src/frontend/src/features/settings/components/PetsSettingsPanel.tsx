import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, Check, ImagePlus, Loader2, PawPrint, Plus, Upload, X } from 'lucide-react'
import { Button } from '@/components/button'
import { Checkbox } from '@/components/checkbox'
import { Input } from '@/components/input'
import { PetThumb } from '@/features/pets/components/PetThumb'
import { loadPetLibrary, subscribePetLibrary, getPetLibrary, type PetEntry } from '@/features/pets/petsStore'
import { deletePet, downloadPetdex, uploadPet } from '@/features/pets/services/petsApi'
import {
  getPetsConfig,
  setPetsEnabled,
  setPetRoster,
  subscribePrefs,
  type PetsConfig,
} from '@/features/prefs/stores/prefsStore'
import { cn } from '@/features/shared/utils/utils'

type SaveStatus = 'idle' | 'success' | 'error'

const LIBRARY_LIMIT = 20
// The Petdex library has thousands of entries; cap the grid at this many
// results and rely on search to narrow it down.

export function PetsSettingsPanel() {
  const [cfg, setCfg] = useState<PetsConfig>(() => getPetsConfig())
  const [library, setLibrary] = useState<PetEntry[]>(() => getPetLibrary())
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [uploadError, setUploadError] = useState('')
  const [uploadBusy, setUploadBusy] = useState(false)
  const [saveStatus, setSaveStatus] = useState<SaveStatus>('idle')
  const [downloading, setDownloading] = useState<Set<string>>(new Set())
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [overIndex, setOverIndex] = useState<number | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const saveTimerRef = useRef<number | null>(null)

  useEffect(() => {
    setLoading(true)
    void loadPetLibrary().finally(() => setLoading(false))
    return subscribePetLibrary(() => setLibrary(getPetLibrary()))
  }, [])

  useEffect(() => {
    return subscribePrefs(() => setCfg(getPetsConfig()))
  }, [])

  const save = async (fn: () => Promise<boolean>) => {
    const ok = await fn()
    setSaveStatus(ok ? 'success' : 'error')
    if (saveTimerRef.current) window.clearTimeout(saveTimerRef.current)
    saveTimerRef.current = window.setTimeout(() => setSaveStatus('idle'), 1500)
  }

  const toggleEnabled = () => {
    void save(() => setPetsEnabled(!cfg.enabled))
  }

  // Removing a pet from the roster deletes its locally stored copy too, so
  // it disappears from the library completely and can be re-added later.
  const removeFromRoster = (slug: string) => {
    void save(async () => {
      await deletePet(slug)
      await setPetRoster(cfg.roster.filter((s) => s !== slug))
      await loadPetLibrary(true)
      return true
    })
  }

  const reorderRoster = (from: number, to: number) => {
    const next = [...cfg.roster]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    void save(() => setPetRoster(next))
  }

  // Downloading a Petdex pet stores its spritesheet locally (fetched
  // server-side, since the Petdex CDN sends no CORS headers) and adds the
  // local copy to the roster, so the pet keeps working even if Petdex is
  // unreachable.
  const handleDownloadPetdex = async (pet: PetEntry) => {
    setDownloading((prev) => new Set(prev).add(pet.slug))
    setUploadError('')
    try {
      const uploaded = await downloadPetdex(pet.name, pet.slug, pet.spritesheetUrl)
      const roster = cfg.roster.includes(uploaded.id) ? cfg.roster : [...cfg.roster, uploaded.id]
      await setPetRoster(roster)
      await loadPetLibrary(true)
      setSaveStatus('success')
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : 'Download failed')
      setSaveStatus('error')
    } finally {
      setDownloading((prev) => {
        const next = new Set(prev)
        next.delete(pet.slug)
        return next
      })
    }
  }

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!uploadFile) {
      setUploadError('Choose a sprite file first.')
      return
    }
    setUploadBusy(true)
    setUploadError('')
    try {
      const pet = await uploadPet(uploadName.trim(), uploadFile)
      const roster = cfg.roster.includes(pet.id) ? cfg.roster : [...cfg.roster, pet.id]
      await setPetRoster(roster)
      await loadPetLibrary(true)
      setUploadName('')
      setUploadFile(null)
      if (fileInputRef.current) fileInputRef.current.value = ''
      setSaveStatus('success')
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : 'Upload failed')
    } finally {
      setUploadBusy(false)
    }
  }

  const rosterEntries = useMemo(
    () =>
      cfg.roster
        .map((slug) => library.find((p) => p.slug === slug))
        .filter((p): p is PetEntry => Boolean(p)),
    [cfg.roster, library],
  )

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return library
    return library.filter(
      (p) => p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q),
    )
  }, [library, search])

  const visible = useMemo(() => filtered.slice(0, LIBRARY_LIMIT), [filtered])

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="text-sm font-medium mb-1 flex items-center gap-1.5">
          <PawPrint className="h-3.5 w-3.5" /> Pets
        </h3>
        <p className="text-xs text-muted-foreground">
          Pets from{' '}
          <a href="https://petdex.dev" target="_blank" rel="noreferrer" className="text-primary underline">
            petdex.dev
          </a>{' '}
          wander along the bottom of terminal areas and react to your agents.
        </p>
      </div>

      {(saveStatus === 'success' || saveStatus === 'error') && (
        <div className="flex items-center gap-1.5 text-[10px] font-medium shrink-0 -mt-2">
          {saveStatus === 'success' ? (
            <span className="text-emerald-500 flex items-center gap-1">
              <Check className="h-3 w-3" /> Saved
            </span>
          ) : (
            <span className="text-destructive flex items-center gap-1">
              <AlertCircle className="h-3 w-3" /> Save failed
            </span>
          )}
        </div>
      )}

      <label className="flex items-center gap-2.5 cursor-pointer">
        <Checkbox checked={cfg.enabled} onChange={toggleEnabled} />
        <div className="flex flex-col">
          <span className="text-xs font-medium">Enable pets</span>
          <span className="text-[10px] text-muted-foreground">One pet per agent; click to focus terminal.</span>
        </div>
      </label>

      {/* Roster */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-medium">Roster</h4>
          {cfg.roster.length > 1 && (
            <span className="text-[10px] text-muted-foreground">Drag to reorder</span>
          )}
        </div>
        {rosterEntries.length === 0 ? (
          <p className="text-[11px] text-muted-foreground border border-dashed border-border rounded-md px-3 py-3 text-center">
            No pets on the roster yet. Search the library below and add a few.
          </p>
        ) : (
          <div className="grid grid-cols-2 gap-2">
            {rosterEntries.map((entry, i) => (
              <div
                key={entry.slug}
                draggable
                onDragStart={() => setDragIndex(i)}
                onDragOver={(e) => {
                  e.preventDefault()
                  setOverIndex(i)
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  if (dragIndex !== null && dragIndex !== i) reorderRoster(dragIndex, i)
                  setDragIndex(null)
                  setOverIndex(null)
                }}
                onDragEnd={() => {
                  setDragIndex(null)
                  setOverIndex(null)
                }}
                title="Drag to reorder"
                className={cn(
                  'group relative flex flex-col items-center gap-1.5 rounded-md border border-border bg-muted/30 px-2 py-2 select-none cursor-grab active:cursor-grabbing transition-all',
                  dragIndex === i && 'opacity-40',
                  overIndex === i && dragIndex !== null && dragIndex !== i && 'ring-1 ring-primary',
                )}
              >
                <button
                  onClick={() => removeFromRoster(entry.slug)}
                  className="absolute right-1 top-1 z-10 rounded p-0.5 text-muted-foreground/60 opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 cursor-pointer"
                  title="Remove from roster and library"
                >
                  <X className="h-3 w-3" />
                </button>
                <PetThumb entry={entry} />
                <span className="w-full truncate text-center text-[11px] font-medium">{entry.name}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Library picker */}
      <div className="flex flex-col gap-2">
        <h4 className="text-xs font-medium">Pet library</h4>
        <Input
          placeholder="Search pets by name or slug…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="h-8 text-xs"
        />
        {loading && library.length === 0 ? (
          <div className="flex items-center justify-center gap-2 py-6 text-[11px] text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading pet library…
          </div>
        ) : visible.length === 0 ? (
          <div className="py-6 text-center text-[11px] text-muted-foreground">No pets match.</div>
        ) : (
          <div className="grid grid-cols-2 gap-2">
            {visible.map((p) => {
              const isCustom = p.kind === 'custom'
              // A pet counts as "added" when it is on the roster, or when
              // it is already stored locally (downloaded/uploaded) — those
              // never need an Add button, only a way to delete the copy.
              const haveIt = cfg.roster.includes(p.slug) || isCustom
              const isDownloading = downloading.has(p.slug)
              return (
                <div
                  key={p.slug}
                  className="flex flex-col items-center gap-1.5 rounded-md border border-border bg-muted/30 px-2 py-2"
                >
                  <PetThumb entry={p} />
                  <span className="w-full truncate text-center text-[11px] font-medium">{p.name}</span>
                  {haveIt ? (
                    <button
                      onClick={() => void removeFromRoster(p.slug)}
                      className="text-muted-foreground hover:text-destructive transition-colors cursor-pointer p-0.5"
                      title="Remove from roster and library"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 px-2 gap-1 text-[11px]"
                      disabled={isDownloading}
                      onClick={() => void handleDownloadPetdex(p)}
                    >
                      {isDownloading ? (
                        <>
                          <Loader2 className="h-3 w-3 animate-spin" /> Adding
                        </>
                      ) : (
                        <>
                          <Plus className="h-3 w-3" /> Add
                        </>
                      )}
                    </Button>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Upload */}
      <form onSubmit={(e) => void handleUpload(e)} className="flex flex-col gap-2">
        <h4 className="text-xs font-medium">Upload your own</h4>
        <div className="flex items-center gap-2">
          <Input
            placeholder="Pet name (optional)"
            value={uploadName}
            onChange={(e) => setUploadName(e.target.value)}
            className="h-8 text-xs w-40 shrink-0"
          />
          <label className="flex-1 min-w-0">
            <input
              ref={fileInputRef}
              type="file"
              accept=".webp,.png,image/webp,image/png"
              className="hidden"
              onChange={(e) => setUploadFile(e.target.files?.[0] ?? null)}
            />
            <span
              className={cn(
                'flex h-8 items-center gap-1.5 rounded-md border border-input px-2.5 text-xs text-muted-foreground cursor-pointer overflow-hidden whitespace-nowrap',
              )}
              title="Choose sprite file"
            >
              <ImagePlus className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate">{uploadFile ? uploadFile.name : 'Choose sprite…'}</span>
            </span>
          </label>
          <Button type="submit" size="sm" className="h-8 gap-1.5 text-xs shrink-0" disabled={uploadBusy}>
            {uploadBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
            Upload
          </Button>
        </div>
        {uploadError && <p className="text-[11px] text-destructive">{uploadError}</p>}
        <p className="text-[10px] text-muted-foreground">
          A sprite atlas is a 8-column grid of frames. Use a 8x9 (1536×1872) or 8x11 (1536×2288) atlas
          at any clean integer scale.
        </p>
      </form>
    </div>
  )
}
