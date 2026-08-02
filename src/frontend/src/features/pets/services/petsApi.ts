// Client for Caw's own pet endpoints: listing, uploading, deleting and
// serving locally-stored custom pet sprites.

export interface UploadedPet {
  id: string
  name: string
  kind: 'custom'
  spritesheetUrl: string
}

export async function fetchUploadedPets(): Promise<UploadedPet[]> {
  try {
    const res = await fetch('/api/pets')
    if (!res.ok) return []
    const data = (await res.json())?.data
    return Array.isArray(data) ? (data as UploadedPet[]) : []
  } catch {
    return []
  }
}

export async function uploadPet(name: string, file: File): Promise<UploadedPet> {
  const form = new FormData()
  form.append('name', name)
  form.append('file', file)
  const res = await fetch('/api/pets', { method: 'POST', body: form })
  if (!res.ok) {
    let message = `Upload failed (${res.status})`
    try {
      const body = (await res.json()) as { error?: { message?: string } }
      if (body?.error?.message) message = body.error.message
    } catch {
      // fall through to generic message
    }
    throw new Error(message)
  }
  return (await res.json())?.data as UploadedPet
}

export async function deletePet(id: string): Promise<boolean> {
  try {
    const res = await fetch(`/api/pets/${encodeURIComponent(id)}`, { method: 'DELETE' })
    return res.ok
  } catch {
    return false
  }
}
