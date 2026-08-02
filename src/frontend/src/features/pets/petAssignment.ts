import type { PetsConfig } from '@/features/prefs/stores/prefsStore'

// Pets walk on a reserved floor lane below the terminal grid instead of over
// the terminal content. This is the lane height in px; pet sprites are capped
// so they always fit inside it.
export const PET_STRIP_HEIGHT = 128

// petSlugForAgent computes the pet assigned to an agent pane. Precedence:
// a per-agent pinned pet (user-set), then the persisted per-agent assignment
// (survives the terminal being closed), then the roster rotating by the
// pane's ordinal position among agent leaves. Deterministic, no counters.
export function petSlugForAgent(
  agentId: string | undefined,
  ordinal: number,
  cfg: PetsConfig,
): string | undefined {
  if (!agentId) return undefined
  if (cfg.roster.length === 0) return undefined
  const pinned = cfg.agentPins[agentId]
  if (pinned && cfg.roster.includes(pinned)) return pinned
  const assigned = cfg.assignments?.[agentId]
  if (assigned && cfg.roster.includes(assigned)) return assigned
  return cfg.roster[ordinal % cfg.roster.length]
}
