// Animation states for petdex sprites. Each state maps to one row of the
// sprite atlas (8 columns wide) with its own frame count and cycle duration.
// Values mirror https://github.com/crafter-station/petdex (MIT License).

export interface PetStateDef {
  name: string
  row: number
  frames: number
  duration: number
}

export const PET_STATES: PetStateDef[] = [
  { name: 'idle', row: 0, frames: 6, duration: 1100 },
  { name: 'running-right', row: 1, frames: 8, duration: 1060 },
  { name: 'running-left', row: 2, frames: 8, duration: 1060 },
  { name: 'waving', row: 3, frames: 4, duration: 700 },
  { name: 'jumping', row: 4, frames: 5, duration: 840 },
  { name: 'failed', row: 5, frames: 8, duration: 1220 },
  { name: 'waiting', row: 6, frames: 6, duration: 1010 },
  { name: 'running', row: 7, frames: 6, duration: 820 },
  { name: 'review', row: 8, frames: 6, duration: 1030 },
]

export function getPetState(name: string): PetStateDef {
  return PET_STATES.find((s) => s.name === name) ?? PET_STATES[0]
}
