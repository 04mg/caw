import { useEffect, useRef } from 'react'
import {
  getPetsConfig,
  loadPrefs,
  setAgentPetAssignments,
  subscribePrefs,
} from '@/features/prefs/stores/prefsStore'
import { collectAgentLeaves, setLeafPetSlug } from '@/features/shared/utils/layout'
import { type Workspace } from '@/features/workspaces/types'
import { petSlugForAgent } from '../petAssignment'

interface Options {
  workspaces: Workspace[]
  patchWorkspace: (id: string, fn: (ws: Workspace) => Workspace) => void
}

// usePetReconciliation keeps each agent pane's persisted petSlug in sync with
// the shared pets config. It runs after prefs load, on any prefs change and
// on any workspace change; it only patches leaves whose slug differs from the
// deterministic assignment (pins first, then persisted per-agent assignments,
// then roster rotation by ordinal), so it converges after a single pass.
//
// Whenever a leaf resolves to a pet without an explicit user pin, the result
// is persisted as that agent's assignment so closing and reopening the agent
// terminal restores the exact same pet instead of dropping it.
export function usePetReconciliation({ workspaces, patchWorkspace }: Options) {
  const ref = useRef({ workspaces, patchWorkspace })
  ref.current = { workspaces, patchWorkspace }

  useEffect(() => {
    let reconciling = false

    const reconcile = () => {
      if (reconciling) return
      const { workspaces: wsList, patchWorkspace: patch } = ref.current
      const cfg = getPetsConfig()
      reconciling = true

      const assignments = { ...(cfg.assignments ?? {}) }
      let assignmentsChanged = false

      for (const ws of wsList) {
        const diffs: { tabId: string; leafId: string; slug: string | undefined }[] = []
        let ordinal = 0
        for (const tab of ws.layouts) {
          for (const leaf of collectAgentLeaves(tab.layout)) {
            const slug = petSlugForAgent(leaf.agentId, ordinal, cfg)
            if (leaf.petSlug !== slug) {
              diffs.push({ tabId: tab.id, leafId: leaf.id, slug })
            }
            ordinal++
            if (leaf.agentId && !cfg.agentPins[leaf.agentId]) {
              if (slug) {
                if (assignments[leaf.agentId] !== slug) {
                  assignments[leaf.agentId] = slug
                  assignmentsChanged = true
                }
              } else if (leaf.agentId in assignments) {
                delete assignments[leaf.agentId]
                assignmentsChanged = true
              }
            }
          }
        }
        if (diffs.length === 0) continue
        patch(ws.id, (w) => {
          let next = w
          for (const d of diffs) {
            next = {
              ...next,
              layouts: next.layouts.map((t) =>
                t.id === d.tabId ? { ...t, layout: setLeafPetSlug(t.layout, d.leafId, d.slug) } : t,
              ),
            }
          }
          return next
        })
      }

      if (assignmentsChanged) void setAgentPetAssignments(assignments)
      reconciling = false
    }

    void loadPrefs().then(reconcile)
    const unsub = subscribePrefs(reconcile)
    return () => unsub()
  }, [workspaces])
}
