// Client for the Petdex pet manifest. Petdex sends no CORS headers, so the
// manifest is proxied through Caw's same-origin /api/pets/petdex-manifest.
// The manifest is large (~1.4MB), so it is cached in memory plus a
// best-effort localStorage cache with a 24h TTL.

export interface PetdexPet {
  slug: string
  displayName: string
  kind: string
  submittedBy?: string
  spritesheetUrl: string
}

const MANIFEST_URL = '/api/pets/petdex-manifest'
const CACHE_TTL_MS = 24 * 60 * 60 * 1000
const CACHE_KEY = 'caw:petdex-manifest'

interface ManifestCache {
  fetchedAt: number
  pets: PetdexPet[]
}

let memoryCache: ManifestCache | null = null

function readLocal(): ManifestCache | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ManifestCache
    if (parsed && Array.isArray(parsed.pets)) return parsed
  } catch {
    // ignore
  }
  return null
}

export async function fetchPetdexPets(): Promise<PetdexPet[]> {
  if (memoryCache && Date.now() - memoryCache.fetchedAt < CACHE_TTL_MS) {
    return memoryCache.pets
  }
  const local = readLocal()
  if (local && Date.now() - local.fetchedAt < CACHE_TTL_MS) {
    memoryCache = local
    return local.pets
  }
  try {
    const res = await fetch(MANIFEST_URL)
    if (!res.ok) throw new Error(`manifest ${res.status}`)
    const json = (await res.json()) as { data?: { pets?: unknown } }
    const pets = Array.isArray(json.data?.pets) ? (json.data.pets as PetdexPet[]) : []
    memoryCache = { fetchedAt: Date.now(), pets }
    try {
      localStorage.setItem(CACHE_KEY, JSON.stringify(memoryCache))
    } catch {
      // ignore quota errors
    }
    return pets
  } catch (err) {
    console.error('Failed to fetch petdex manifest:', err)
    if (memoryCache) return memoryCache.pets
    if (local) return local.pets
    return []
  }
}
