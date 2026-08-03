import type { PetsConfig } from '@/features/prefs/stores/prefsStore'

// Pets float on a transparent strip over the bottom of the terminal grid
// (they never take layout space). This is the strip height in px; pet
// sprites are capped so they always fit inside it.
export const PET_STRIP_HEIGHT = 64

// petSlugForAgent computes the pet assigned to an agent pane. A per-agent
// pinned pet wins, otherwise the roster rotates by the pane's ordinal
// position among agent leaves in the layout tree. Deterministic, no counters.
export function petSlugForAgent(
  agentId: string | undefined,
  ordinal: number,
  cfg: PetsConfig,
): string | undefined {
  if (!agentId) return undefined
  if (cfg.roster.length === 0) return undefined
  const pinned = cfg.agentPins[agentId]
  if (pinned && cfg.roster.includes(pinned)) return pinned
  return cfg.roster[ordinal % cfg.roster.length]
}
