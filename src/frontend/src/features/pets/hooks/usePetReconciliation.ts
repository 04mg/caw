import { useEffect, useRef } from 'react'
import { getPetsConfig, loadPrefs, subscribePrefs } from '@/features/prefs/stores/prefsStore'
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
// deterministic assignment (pins first, else roster rotation by ordinal), so
// it converges after a single pass.
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
      reconciling = false
    }

    void loadPrefs().then(reconcile)
    const unsub = subscribePrefs(reconcile)
    return () => unsub()
  }, [workspaces])
}
