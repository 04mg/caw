import { type TabGroupsNode, type Workspace } from '../types'

export function getDefaultTabGroups(tabIds: string[], activeTabIndex: number): TabGroupsNode {
  return {
    type: 'group',
    id: crypto.randomUUID(),
    tabs: tabIds,
    activeTabIndex: Math.max(0, Math.min(activeTabIndex, tabIds.length - 1)),
  }
}

export function collectTabIds(node: TabGroupsNode): string[] {
  if (node.type === 'group') {
    return node.tabs
  }
  return node.children.flatMap(collectTabIds)
}

export function collectGroups(node: TabGroupsNode): Extract<TabGroupsNode, { type: 'group' }>[] {
  if (node.type === 'group') {
    return [node]
  }
  return node.children.flatMap(collectGroups)
}

/**
 * Returns the ID of the group that occupies the top-right corner of the screen.
 * At each horizontal split, we take the rightmost child.
 * At each vertical split, we take the first (topmost) child.
 */
export function getTopRightGroupId(node: TabGroupsNode): string {
  if (node.type === 'group') {
    return node.id
  }
  if (node.orientation === 'horizontal') {
    // rightmost child
    return getTopRightGroupId(node.children[node.children.length - 1])
  }
  // vertical split — take the first (top) child
  return getTopRightGroupId(node.children[0])
}

export function findGroupWithTab(node: TabGroupsNode, tabId: string): Extract<TabGroupsNode, { type: 'group' }> | null {
  if (node.type === 'group') {
    return node.tabs.includes(tabId) ? node : null
  }
  for (const child of node.children) {
    const found = findGroupWithTab(child, tabId)
    if (found) return found
  }
  return null
}

export function findGroupById(node: TabGroupsNode, groupId: string): Extract<TabGroupsNode, { type: 'group' }> | null {
  if (node.type === 'group') {
    return node.id === groupId ? node : null
  }
  for (const child of node.children) {
    const found = findGroupById(child, groupId)
    if (found) return found
  }
  return null
}

/**
 * Sanitizes a tab groups tree:
 * 1. Filters out any tab ID not in validTabIds.
 * 2. Simplifies splits (flattens single-child splits, removes empty children).
 * Returns null if the group has become entirely empty (and can be pruned).
 */
export function sanitizeTabGroups(
  node: TabGroupsNode,
  validTabIds: string[],
): TabGroupsNode | null {
  if (node.type === 'group') {
    const tabs = node.tabs.filter((t) => validTabIds.includes(t))
    if (tabs.length === 0) {
      return null
    }
    const activeTabIndex = Math.max(0, Math.min(node.activeTabIndex, tabs.length - 1))
    return { ...node, tabs, activeTabIndex }
  }

  const children: TabGroupsNode[] = []
  const sizes: number[] = []

  for (let i = 0; i < node.children.length; i++) {
    const child = sanitizeTabGroups(node.children[i], validTabIds)
    if (child) {
      children.push(child)
      sizes.push(node.sizes[i] ?? (100 / node.children.length))
    }
  }

  if (children.length === 0) {
    return null
  }

  if (children.length === 1) {
    return children[0]
  }

  // Normalize sizes to sum to 100
  const total = sizes.reduce((a, b) => a + b, 0) || 1
  const normalizedSizes = sizes.map((s) => (s / total) * 100)

  return {
    ...node,
    children,
    sizes: normalizedSizes,
  }
}

/**
 * Prepares and ensures a valid tab groups tree for a workspace.
 */
export function ensureTabGroups(workspace: Workspace): { tree: TabGroupsNode; activeGroupId: string } {
  const layouts = workspace.layouts || []
  const layoutIds = layouts.map((l) => l.id)

  let tree: TabGroupsNode | null = null

  if (workspace.tabGroups) {
    tree = workspace.tabGroups
  } else if (workspace.tabGroupsJson) {
    try {
      tree = JSON.parse(workspace.tabGroupsJson) as TabGroupsNode
    } catch {
      tree = null
    }
  }

  if (!tree) {
    tree = getDefaultTabGroups(layoutIds, workspace.activeTabIndex ?? 0)
  } else {
    // Sanitize tree
    tree = sanitizeTabGroups(tree, layoutIds) || getDefaultTabGroups(layoutIds, 0)
  }

  // If any tabs are not present in the tree, add them to the currently active group or the first group
  const groupedIds = new Set(collectTabIds(tree))
  const missingIds = layoutIds.filter((id) => !groupedIds.has(id))

  if (missingIds.length > 0) {
    let activeGroup: Extract<TabGroupsNode, { type: 'group' }> | null = null
    if (workspace.activeGroupId) {
      activeGroup = findGroupById(tree, workspace.activeGroupId)
    }
    if (!activeGroup) {
      const groups = collectGroups(tree)
      activeGroup = groups[0] || null
    }

    if (activeGroup) {
      const targetId = activeGroup.id
      function addMissing(n: TabGroupsNode): TabGroupsNode {
        if (n.type === 'group' && n.id === targetId) {
          return { ...n, tabs: [...n.tabs, ...missingIds] }
        }
        if (n.type === 'split') {
          return { ...n, children: n.children.map(addMissing) }
        }
        return n
      }
      tree = addMissing(tree)
    }
  }

  // Ensure activeGroupId is valid
  let activeGroupId = workspace.activeGroupId || ''
  let activeGroup = findGroupById(tree, activeGroupId)
  if (!activeGroup) {
    const groups = collectGroups(tree)
    activeGroupId = groups[0]?.id || 'default-group'
  }

  return { tree, activeGroupId }
}

/**
 * Removes a tab from the tree, sanitizing afterwards.
 */
export function removeTabFromTree(node: TabGroupsNode, tabId: string, validTabIds: string[]): TabGroupsNode {
  function doRemove(n: TabGroupsNode): TabGroupsNode | null {
    if (n.type === 'group') {
      const tabs = n.tabs.filter((t) => t !== tabId && validTabIds.includes(t))
      const activeTabIndex = Math.max(0, Math.min(n.activeTabIndex, tabs.length - 1))
      return { ...n, tabs, activeTabIndex }
    }

    const children: TabGroupsNode[] = []
    const sizes: number[] = []

    for (let i = 0; i < n.children.length; i++) {
      const child = doRemove(n.children[i])
      if (child) {
        // If a child is a group with no tabs, prune it only if it's not the last group overall.
        if (child.type === 'group' && child.tabs.length === 0) {
          // If we prune it, we don't push it
          continue
        }
        children.push(child)
        sizes.push(n.sizes[i] ?? (100 / n.children.length))
      }
    }

    if (children.length === 0) return null
    if (children.length === 1) return children[0]

    const total = sizes.reduce((a, b) => a + b, 0) || 1
    const normalizedSizes = sizes.map((s) => (s / total) * 100)

    return { ...n, children, sizes: normalizedSizes }
  }

  const result = doRemove(node)
  if (!result || (result.type === 'group' && result.tabs.length === 0)) {
    return getDefaultTabGroups(validTabIds.filter((t) => t !== tabId), 0)
  }
  return result
}

/**
 * Adds or moves a tab to a specific group.
 * If the tab was elsewhere, it is removed from its old position first.
 */
export function moveTabToGroup(
  node: TabGroupsNode,
  tabId: string,
  targetGroupId: string,
  targetIndex?: number,
): TabGroupsNode {
  // First, remove the tab from its existing position in the tree
  function removeTab(n: TabGroupsNode): TabGroupsNode {
    if (n.type === 'group') {
      const idx = n.tabs.indexOf(tabId)
      if (idx >= 0) {
        const tabs = n.tabs.filter((t) => t !== tabId)
        let activeTabIndex = n.activeTabIndex
        if (activeTabIndex >= tabs.length) {
          activeTabIndex = Math.max(0, tabs.length - 1)
        }
        return { ...n, tabs, activeTabIndex }
      }
      return n
    }
    return { ...n, children: n.children.map(removeTab) }
  }

  const treeWithoutTab = removeTab(node)

  // Next, insert the tab into the target group
  function insertTab(n: TabGroupsNode): TabGroupsNode {
    if (n.type === 'group') {
      if (n.id === targetGroupId) {
        const tabs = [...n.tabs]
        const insertIdx = targetIndex !== undefined ? targetIndex : tabs.length
        tabs.splice(insertIdx, 0, tabId)
        return {
          ...n,
          tabs,
          activeTabIndex: insertIdx,
        }
      }
      return n
    }
    return { ...n, children: n.children.map(insertTab) }
  }

  return insertTab(treeWithoutTab)
}

/**
 * Splits a group to create a new group with the dragged tab.
 */
export function splitGroup(
  node: TabGroupsNode,
  targetGroupId: string,
  draggedTabId: string,
  orientation: 'horizontal' | 'vertical',
  position: 'left' | 'right' | 'top' | 'bottom',
  validTabIds: string[],
): TabGroupsNode {
  const newGroupId = crypto.randomUUID()
  const newGroup: TabGroupsNode = {
    type: 'group',
    id: newGroupId,
    tabs: [draggedTabId],
    activeTabIndex: 0,
  }

  // Remove dragged tab from existing positions first
  const treeWithoutTab = removeTabFromTree(node, draggedTabId, validTabIds)

  // Find target group and replace with a split
  function doSplit(n: TabGroupsNode): TabGroupsNode {
    if (n.type === 'group') {
      if (n.id === targetGroupId) {
        const isBefore = position === 'left' || position === 'top'
        return {
          type: 'split',
          id: crypto.randomUUID(),
          orientation,
          children: isBefore ? [newGroup, n] : [n, newGroup],
          sizes: [50, 50],
        }
      }
      return n
    }

    return {
      ...n,
      children: n.children.map(doSplit),
    }
  }

  return doSplit(treeWithoutTab)
}
