import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, Check, ImagePlus, Loader2, PawPrint, Plus, Upload, X } from 'lucide-react'
import { Button } from '@/components/button'
import { Checkbox } from '@/components/checkbox'
import { Input } from '@/components/input'
import { ScrollArea } from '@/components/scroll-area'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/select'
import { agentTypes } from '@/features/agents/services/agentTypes'
import { loadPetLibrary, subscribePetLibrary, getPetLibrary, type PetEntry } from '@/features/pets/petsStore'
import { deletePet, downloadPetdex, uploadPet } from '@/features/pets/services/petsApi'
import {
  getPetsConfig,
  setAgentPetPin,
  setPetsEnabled,
  setPetRoster,
  subscribePrefs,
  type PetsConfig,
} from '@/features/prefs/stores/prefsStore'
import { cn } from '@/features/shared/utils/utils'

type SaveStatus = 'idle' | 'success' | 'error'

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
  // The Petdex library has thousands of entries; render it in pages so the
  // settings dialog stays responsive.
  const [visibleCount, setVisibleCount] = useState(50)
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

  const removeFromRoster = (slug: string) => {
    void save(() => setPetRoster(cfg.roster.filter((s) => s !== slug)))
  }

  const setPin = (agentId: string, value: string) => {
    void save(() => setAgentPetPin(agentId, value === '' ? null : value))
  }

  const handleDeleteUploaded = async (pet: PetEntry) => {
    await deletePet(pet.slug)
    if (cfg.roster.includes(pet.slug)) {
      await setPetRoster(cfg.roster.filter((s) => s !== pet.slug))
    }
    await loadPetLibrary(true)
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

  const nameBySlug = useMemo(() => {
    const map = new Map<string, string>()
    for (const p of library) map.set(p.slug, p.name)
    return map
  }, [library])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return library
    return library.filter(
      (p) => p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q),
    )
  }, [library, search])

  const visible = useMemo(() => filtered.slice(0, visibleCount), [filtered, visibleCount])

  const agentOptions = Object.values(agentTypes).filter((a) => a.id !== 'terminal')

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="text-sm font-medium mb-1 flex items-center gap-1.5">
          <PawPrint className="h-3.5 w-3.5" /> Pets
        </h3>
        <p className="text-xs text-muted-foreground">
          Cute pets from{' '}
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
          <span className="text-xs font-medium">Enable pets in terminal areas</span>
          <span className="text-[10px] text-muted-foreground">
            Assign one pet per agent pane; click a pet to focus its terminal.
          </span>
        </div>
      </label>

      {/* Roster */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-medium">Roster</h4>
          <span className="text-[10px] text-muted-foreground">
            {cfg.roster.length} pet{cfg.roster.length === 1 ? '' : 's'} — rotation order
          </span>
        </div>
        {cfg.roster.length === 0 ? (
          <p className="text-[11px] text-muted-foreground border border-dashed border-border rounded-md px-3 py-3 text-center">
            No pets on the roster yet. Search the library below and add a few.
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {cfg.roster.map((slug) => (
              <span
                key={slug}
                className="inline-flex items-center gap-1.5 rounded-md border border-border bg-muted/30 px-2 py-1 text-[11px]"
              >
                <span className="max-w-[160px] truncate">{nameBySlug.get(slug) ?? slug}</span>
                <button
                  onClick={() => removeFromRoster(slug)}
                  className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  title="Remove from roster"
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
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
          onChange={(e) => {
            setSearch(e.target.value)
            setVisibleCount(50)
          }}
          className="h-8 text-xs"
        />
        <div className="rounded-md border border-border">
          <ScrollArea className="h-48">
            <div className="flex flex-col">
              {loading && library.length === 0 ? (
                <div className="flex items-center justify-center gap-2 py-6 text-[11px] text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading pet library…
                </div>
              ) : visible.length === 0 ? (
                <div className="py-6 text-center text-[11px] text-muted-foreground">
                  No pets match.
                </div>
              ) : (
                <>
                  {visible.map((p) => {
                  const isCustom = p.kind === 'custom'
                  // A pet counts as "added" when it is on the roster, or when
                  // it is already stored locally (downloaded/uploaded) — those
                  // never need an Add button, only a way to delete the copy.
                  const haveIt = cfg.roster.includes(p.slug) || isCustom
                  const isDownloading = downloading.has(p.slug)
                  const origin = isCustom ? (p.source ? 'downloaded' : 'uploaded') : 'petdex'
                  return (
                    <div
                      key={p.slug}
                      className="flex items-center gap-2 px-2.5 py-1.5 text-[11px] border-b border-border/50 last:border-b-0 hover:bg-accent/30"
                    >
                      <span className="flex-1 min-w-0">
                        <span className="block truncate font-medium">{p.name}</span>
                        <span className="block truncate text-[9px] text-muted-foreground">
                          {p.slug}
                          <span className="ml-1 text-[8px] uppercase">{origin}</span>
                        </span>
                      </span>
                      {haveIt ? (
                        <>
                          {isCustom && (
                            <button
                              onClick={() => void handleDeleteUploaded(p)}
                              className="text-muted-foreground hover:text-destructive transition-colors cursor-pointer p-0.5"
                              title="Delete uploaded pet"
                            >
                              <X className="h-3 w-3" />
                            </button>
                          )}
                          <span className="text-emerald-500 flex items-center gap-0.5">
                            <Check className="h-3 w-3" /> Added
                          </span>
                        </>
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
                </>
              )}
            </div>
          </ScrollArea>
          {filtered.length > visibleCount && (
            <button
              onClick={() => setVisibleCount((c) => c + 100)}
              className="w-full border-t border-border/50 py-1.5 text-[11px] text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors cursor-pointer"
            >
              Show {Math.min(100, filtered.length - visibleCount)} more of {filtered.length}
            </button>
          )}
        </div>
      </div>

      {/* Agent pins */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-medium">Agent pinning</h4>
          <span className="text-[10px] text-muted-foreground">Pin a pet to a specific agent</span>
        </div>
        {agentOptions.length === 0 ? null : (
          <div className="flex flex-col gap-1.5">
            {agentOptions.map((agent) => (
              <div key={agent.id} className="flex items-center gap-2">
                <span className="w-28 shrink-0 truncate text-[11px] text-muted-foreground">
                  {agent.label}
                </span>
                <Select
                  value={cfg.agentPins[agent.id] ?? ''}
                  onValueChange={(v) => setPin(agent.id, v)}
                >
                  <SelectTrigger className="flex-1 h-8 text-xs">
                    <SelectValue placeholder="Auto (rotation)" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="">Auto (rotation)</SelectItem>
                    {cfg.roster.map((slug) => (
                      <SelectItem key={slug} value={slug}>
                        {nameBySlug.get(slug) ?? slug}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Upload */}
      <form onSubmit={(e) => void handleUpload(e)} className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-medium">Upload your own</h4>
          <span className="text-[10px] text-muted-foreground">WebP or PNG sprite atlas</span>
        </div>
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
