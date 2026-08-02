import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useAgentStatuses } from '@/features/agents/hooks/useAgentStatuses'
import { subscribePrefs, loadPrefs, getPetsConfig } from '@/features/prefs/stores/prefsStore'
import { collectAgentLeaves, type LayoutNode } from '@/features/shared/utils/layout'
import { getPetEntry, loadPetLibrary, subscribePetLibrary, type PetEntry } from '../petsStore'
import { Pet, type PetRange } from './Pet'

interface PetStageProps {
  layout: LayoutNode
  onFocusLeaf?: (leafId: string) => void
}

interface StagePet {
  leaf: Extract<LayoutNode, { type: 'leaf' }>
  pet: PetEntry
}

interface PaneRect {
  x: number
  y: number
  w: number
  h: number
}

export function PetStage({ layout, onFocusLeaf }: PetStageProps) {
  const [prefsVersion, setPrefsVersion] = useState(0)
  const [libraryVersion, setLibraryVersion] = useState(0)
  const [size, setSize] = useState({ w: 0, h: 0 })
  const [paneRects, setPaneRects] = useState<Record<string, PaneRect>>({})
  const containerRef = useRef<HTMLDivElement | null>(null)
  const occupancyRef = useRef<Record<string, PetRange>>({})

  const statuses = useAgentStatuses()

  useEffect(() => {
    void loadPrefs().then(() => setPrefsVersion((v) => v + 1))
    return subscribePrefs(() => setPrefsVersion((v) => v + 1))
  }, [])

  useEffect(() => {
    void loadPetLibrary()
    return subscribePetLibrary(() => setLibraryVersion((v) => v + 1))
  }, [])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      const r = entries[0]?.contentRect
      if (r) setSize({ w: r.width, h: r.height })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // Measure each agent leaf's pane bounding box relative to the stage so a
  // pet stands on top of the terminal it is assigned to, while the sprite is
  // still drawn in the full-content-area overlay above every pane.
  const measurePanes = useCallback(() => {
    const el = containerRef.current
    if (!el) return
    const cRect = el.getBoundingClientRect()
    const next: Record<string, PaneRect> = {}
    for (const leaf of collectAgentLeaves(layout)) {
      const pane = el.querySelector(`[data-pane-id="${leaf.id}"]`)
      // Fall back to the full stage (e.g. the mobile view, which renders the
      // terminal without a data-pane-id wrapper).
      if (!pane) {
        next[leaf.id] = { x: 0, y: 0, w: cRect.width, h: cRect.height }
        continue
      }
      const r = pane.getBoundingClientRect()
      next[leaf.id] = { x: r.left - cRect.left, y: r.top - cRect.top, w: r.width, h: r.height }
    }
    setPaneRects(next)
  }, [layout])

  useEffect(() => {
    measurePanes()
  }, [measurePanes, size])

  // Re-measure when panes resize (splitter drags change pane bounds without
  // resizing the stage itself).
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const ro = new ResizeObserver(() => measurePanes())
    const ids = new Set<string>()
    for (const leaf of collectAgentLeaves(layout)) ids.add(leaf.id)
    for (const id of ids) {
      const pane = el.querySelector(`[data-pane-id="${id}"]`)
      if (pane) ro.observe(pane)
    }
    return () => ro.disconnect()
  }, [layout, measurePanes, size])

  const pets: StagePet[] = useMemo(() => {
    const cfg = getPetsConfig()
    if (!cfg.enabled || cfg.roster.length === 0) return []
    const out: StagePet[] = []
    for (const leaf of collectAgentLeaves(layout)) {
      if (!leaf.petSlug || !cfg.roster.includes(leaf.petSlug)) continue
      const pet = getPetEntry(leaf.petSlug)
      if (pet) out.push({ leaf, pet })
    }
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prefsVersion, libraryVersion, layout])

  // Stable per-leaf callbacks so status re-renders never restart the pets'
  // animation loops (Pet writes its pose to occupancyRef every frame).
  const poses = useMemo(() => {
    const map = new Map<string, (pose: PetRange) => void>()
    for (const { leaf } of pets) {
      map.set(leaf.id, (pose) => {
        occupancyRef.current[leaf.id] = pose
      })
    }
    return map
  }, [pets])

  const ranges = useMemo(() => {
    const map = new Map<string, () => PetRange[]>()
    for (const { leaf } of pets) {
      map.set(leaf.id, () => {
        const others: PetRange[] = []
        for (const [id, r] of Object.entries(occupancyRef.current)) {
          if (id !== leaf.id && r.w > 0) others.push(r)
        }
        return others
      })
    }
    return map
  }, [pets])

  if (!getPetsConfig().enabled) return null
  if (pets.length === 0) return null

  return (
    <div ref={containerRef} className="pointer-events-none absolute inset-0 z-[5] overflow-hidden" aria-hidden="true">
      {size.w > 0 &&
        size.h > 0 &&
        pets.map(({ leaf, pet }) => {
          const rect = paneRects[leaf.id]
          if (!rect) return null
          return (
            <Pet
              key={leaf.id}
              pet={pet}
              leafId={leaf.id}
              status={statuses[leaf.id]}
              x={rect.x}
              y={rect.y}
              containerW={rect.w}
              containerH={rect.h}
              getOtherRanges={ranges.get(leaf.id) ?? (() => [])}
              onPose={poses.get(leaf.id) ?? (() => {})}
              onClick={() => onFocusLeaf?.(leaf.id)}
            />
          )
        })}
    </div>
  )
}
