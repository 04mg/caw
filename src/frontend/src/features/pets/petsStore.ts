import { fetchPetdexPets } from './services/petdexApi'
import { fetchUploadedPets } from './services/petsApi'

export interface PetEntry {
  slug: string
  name: string
  kind: string
  spritesheetUrl: string
}

let library: PetEntry[] | null = null
let loading: Promise<PetEntry[]> | null = null
const listeners = new Set<() => void>()

// loadPetLibrary fetches the combined pet library (Petdex manifest merged
// with locally uploaded pets). Results are cached module-wide so the pet
// stage and the settings panel share one fetch.
export async function loadPetLibrary(force = false): Promise<PetEntry[]> {
  if (library && !force) return library
  if (loading && !force) return loading
  loading = (async () => {
    try {
      const [petdex, uploaded] = await Promise.all([fetchPetdexPets(), fetchUploadedPets()])
      const map = new Map<string, PetEntry>()
      for (const p of petdex) {
        map.set(p.slug, {
          slug: p.slug,
          name: p.displayName,
          kind: p.kind || 'pet',
          spritesheetUrl: p.spritesheetUrl,
        })
      }
      for (const p of uploaded) {
        map.set(p.id, {
          slug: p.id,
          name: p.name,
          kind: p.kind,
          spritesheetUrl: p.spritesheetUrl,
        })
      }
      library = [...map.values()]
    } catch {
      library = library ?? []
    } finally {
      loading = null
      for (const l of listeners) l()
    }
    return library
  })()
  return loading
}

export function getPetLibrary(): PetEntry[] {
  return library ?? []
}

export function getPetEntry(slug: string | undefined): PetEntry | undefined {
  if (!slug || !library) return undefined
  return library.find((p) => p.slug === slug)
}

export function subscribePetLibrary(cb: () => void): () => void {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}
