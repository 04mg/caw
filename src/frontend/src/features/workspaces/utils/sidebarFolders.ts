import { type Workspace, type WorkspaceFolder } from '../types'

// A single visible row in the workspace sidebar. Folders and loose
// workspaces are interleaved at the root level; workspaces inside a folder
// are rendered one indentation level deeper, right after their folder.
export type SidebarRow =
  | { kind: 'folder'; folder: WorkspaceFolder; collapsed: boolean; childCount: number }
  | { kind: 'workspace'; ws: Workspace; depth: number }

// Normalize the sidebar state so invariants always hold:
// - every folderId points at an existing folder (else the workspace becomes root)
// - every root entry (folder or loose workspace) appears exactly once in order
// - stale entries in `order` are dropped; missing ones appended at the end.
// Returns null when nothing had to change so callers can avoid re-render loops.
export function normalizeSidebar(
  workspaces: Workspace[],
  folders: WorkspaceFolder[],
  order: string[],
): { workspaces: Workspace[]; folders: WorkspaceFolder[]; order: string[] } | null {
  const folderIds = new Set(folders.map((f) => f.id))
  let changed = false

  const nextWorkspaces = workspaces.map((ws) => {
    if (ws.folderId && !folderIds.has(ws.folderId)) {
      changed = true
      return { ...ws, folderId: undefined }
    }
    return ws
  })

  // Root entries: folders + workspaces without a folder, in `order` first,
  // then any missing ones appended (preserving flat array order for those).
  const rootWsIds = new Set(nextWorkspaces.filter((w) => !w.folderId).map((w) => w.id))
  const seen = new Set<string>()
  const nextOrder: string[] = []
  for (const id of order) {
    if ((folderIds.has(id) || rootWsIds.has(id)) && !seen.has(id)) {
      seen.add(id)
      nextOrder.push(id)
    } else {
      changed = true
    }
  }
  for (const f of folders) {
    if (!seen.has(f.id)) {
      seen.add(f.id)
      nextOrder.push(f.id)
      changed = true
    }
  }
  for (const w of nextWorkspaces) {
    if (!w.folderId && !seen.has(w.id)) {
      seen.add(w.id)
      nextOrder.push(w.id)
      changed = true
    }
  }

  if (!changed) return null
  return { workspaces: nextWorkspaces, folders, order: nextOrder }
}

// Build the flat list of visible rows for rendering. Collapsed folders hide
// their children but still show as rows themselves.
export function buildSidebarRows(
  workspaces: Workspace[],
  folders: WorkspaceFolder[],
  order: string[],
  collapsedIds: ReadonlySet<string>,
): SidebarRow[] {
  const byId = new Map(workspaces.map((w) => [w.id, w]))
  const childrenOf = new Map<string, Workspace[]>()
  for (const w of workspaces) {
    if (!w.folderId) continue
    const list = childrenOf.get(w.folderId) ?? []
    list.push(w)
    childrenOf.set(w.folderId, list)
  }

  const rows: SidebarRow[] = []
  for (const id of order) {
    const folder = folders.find((f) => f.id === id)
    if (folder) {
      const children = childrenOf.get(folder.id) ?? []
      rows.push({
        kind: 'folder',
        folder,
        collapsed: collapsedIds.has(folder.id),
        childCount: children.length,
      })
      if (!collapsedIds.has(folder.id)) {
        for (const child of children) {
          rows.push({ kind: 'workspace', ws: child, depth: 1 })
        }
      }
      continue
    }
    const ws = byId.get(id)
    if (ws && !ws.folderId) {
      rows.push({ kind: 'workspace', ws, depth: 0 })
    }
  }
  return rows
}

export interface SidebarState {
  workspaces: Workspace[]
  folders: WorkspaceFolder[]
  order: string[]
}

function reorder<T>(list: T[], from: number, to: number): T[] {
  const next = list.slice()
  const [moved] = next.splice(from, 1)
  next.splice(to, 0, moved)
  return next
}

// Move a root-level entry (folder or loose workspace) within the root order.
// Indices refer to positions in the current `order` array of ROOT entries —
// i.e. the sequence of ids itself, not visible rows (a folder's children
// travel with it implicitly since membership is unchanged).
export function moveRootEntry(state: SidebarState, fromId: string, toIndex: number): SidebarState {
  const from = state.order.indexOf(fromId)
  if (from < 0) return state
  const clamped = Math.max(0, Math.min(state.order.length - 1, toIndex))
  if (clamped === from) return state
  return { ...state, order: reorder(state.order, from, clamped) }
}

// Insert a dragged row relative to a target root entry ("before"/"after").
// Dropping relative to a folder's child re-targets the folder itself so the
// drop lands among root entries or inside the parent folder appropriately.
export function moveEntryRelative(
  state: SidebarState,
  dragId: string,
  targetId: string,
  position: 'before' | 'after',
): SidebarState {
  if (dragId === targetId) return state
  const from = state.order.indexOf(dragId)
  if (from < 0) return state
  const target = state.order.indexOf(targetId)
  if (target < 0) return state
  const to = position === 'before' ? target : target + 1
  if (to === from || to === from + 1) return state
  const removed = state.order.slice()
  removed.splice(from, 1)
  const adjustedTarget = target > from ? target - 1 : target
  const insertAt = position === 'before' ? adjustedTarget : adjustedTarget + 1
  removed.splice(insertAt, 0, dragId)
  return { ...state, order: removed }
}

// Move a workspace into a folder (or out to the root when folderId is null).
// A workspace entering the root is appended after its previous root anchor;
// one leaving a folder keeps its flat-array position (children render in
// flat-array order), so only folderId changes here.
export function setWorkspaceFolder(
  state: SidebarState,
  wsId: string,
  folderId: string | null,
): SidebarState {
  const ws = state.workspaces.find((w) => w.id === wsId)
  if (!ws) return state
  if ((ws.folderId ?? null) === folderId) return state

  const workspaces = state.workspaces.map((w) =>
    w.id === wsId ? { ...w, folderId: folderId ?? undefined } : w,
  )

  let order = state.order
  if (folderId === null) {
    // Leaving a folder: make sure it has a root position. Anchor right after
    // the folder it just left (or append at the end).
    if (!order.includes(wsId)) {
      const prevFolderId = ws.folderId
      const anchorIdx = prevFolderId ? order.indexOf(prevFolderId) : -1
      if (anchorIdx >= 0) {
        order = [...order.slice(0, anchorIdx + 1), wsId, ...order.slice(anchorIdx + 1)]
      } else {
        order = [...order, wsId]
      }
    }
  } else {
    // Entering a folder: remove from root order if present.
    order = order.filter((id) => id !== wsId)
  }
  return { ...state, workspaces, order }
}

export function addFolder(state: SidebarState, folder: WorkspaceFolder): SidebarState {
  return { ...state, folders: [...state.folders, folder], order: [...state.order, folder.id] }
}

export function updateFolder(
  state: SidebarState,
  folderId: string,
  name: string,
  emoji?: string,
): SidebarState {
  return {
    ...state,
    folders: state.folders.map((f) => (f.id === folderId ? { ...f, name, emoji } : f)),
  }
}

// Delete a folder AND every workspace inside it (confirmed by the user).
// Terminal cleanup for the deleted workspaces is handled by the caller.
export function deleteFolder(state: SidebarState, folderId: string): SidebarState {
  const removedIds = new Set(
    state.workspaces.filter((w) => w.folderId === folderId).map((w) => w.id),
  )
  return {
    workspaces: state.workspaces.filter((w) => w.folderId !== folderId),
    folders: state.folders.filter((f) => f.id !== folderId),
    order: state.order.filter((id) => id !== folderId && !removedIds.has(id)),
  }
}

// Reorder the flat workspaces array so wsId lands immediately before/after
// targetWsId. Children of a folder render in flat-array order, so this is
// how siblings inside a folder (or a newcomer entering one) get positioned.
export function placeAdjacentFlat(
  state: SidebarState,
  wsId: string,
  targetWsId: string,
  position: 'before' | 'after',
): SidebarState {
  if (wsId === targetWsId) return state
  const from = state.workspaces.findIndex((w) => w.id === wsId)
  const target = state.workspaces.findIndex((w) => w.id === targetWsId)
  if (from < 0 || target < 0) return state
  const moved = state.workspaces[from]
  const removed = state.workspaces.slice()
  removed.splice(from, 1)
  const tAdj = target > from ? target - 1 : target
  removed.splice(position === 'before' ? tAdj : tAdj + 1, 0, moved)
  return { ...state, workspaces: removed }
}

// Move a workspace into a folder, placing it after the folder's last
// existing child in the flat array so the resulting sibling order is
// predictable (it appears at the bottom of the folder).
export function moveToFolderEnd(state: SidebarState, wsId: string, folderId: string): SidebarState {
  const withMembership = setWorkspaceFolder(state, wsId, folderId)
  let lastChild: Workspace | undefined
  for (const w of withMembership.workspaces) {
    if (w.folderId === folderId && w.id !== wsId) lastChild = w
  }
  if (!lastChild) return withMembership
  return placeAdjacentFlat(withMembership, wsId, lastChild.id, 'after')
}
