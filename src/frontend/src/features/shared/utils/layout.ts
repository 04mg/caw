export type LayoutNode =
  | { type: 'leaf'; id: string; cwd: string; cmd?: string[]; agentId?: string; filePath?: string; isDiff?: boolean; agentBranch?: string; baseBranch?: string }
  | { type: 'split'; id: string; orientation: 'horizontal' | 'vertical'; children: LayoutNode[]; sizes: number[] }
  | { type: 'empty' }

export function createEmpty(): LayoutNode {
  return { type: 'empty' }
}

export function normalizeLayout(node: unknown): LayoutNode {
  if (!node || typeof node !== 'object') return createEmpty()
  const n = node as Record<string, unknown>
  if (n.type === 'empty') return { type: 'empty' }
  if (n.type === 'leaf') {
    return {
      type: 'leaf',
      id: typeof n.id === 'string' ? n.id : crypto.randomUUID(),
      cwd: typeof n.cwd === 'string' ? n.cwd : '',
      cmd: Array.isArray(n.cmd) ? (n.cmd as string[]) : undefined,
      agentId: typeof n.agentId === 'string' ? n.agentId : undefined,
      filePath: typeof n.filePath === 'string' ? n.filePath : undefined,
      isDiff: typeof n.isDiff === 'boolean' ? n.isDiff : undefined,
      agentBranch: typeof n.agentBranch === 'string' ? n.agentBranch : undefined,
      baseBranch: typeof n.baseBranch === 'string' ? n.baseBranch : undefined,
    }
  }
  if (n.type === 'split') {
    const rawChildren = Array.isArray(n.children) ? n.children : []
    const children = rawChildren.map(normalizeLayout)
    if (children.length === 0) return createEmpty()
    if (children.length === 1) return children[0]
    const count = children.length
    const sizes = Array.isArray(n.sizes) && (n.sizes as number[]).length === count
      ? (n.sizes as number[])
      : children.map(() => 100 / count)
    return {
      type: 'split',
      id: typeof n.id === 'string' ? n.id : crypto.randomUUID(),
      orientation: n.orientation === 'horizontal' || n.orientation === 'vertical' ? n.orientation : 'horizontal',
      children,
      sizes,
    }
  }
  return createEmpty()
}

export function createLeaf(cwd: string, cmd?: string[], agentId?: string): LayoutNode {
  return { type: 'leaf', id: crypto.randomUUID(), cwd, cmd, agentId }
}

export function splitLeaf(
  root: LayoutNode,
  targetId: string,
  orientation: 'horizontal' | 'vertical',
  cwd: string,
): { node: LayoutNode; newLeafId: string } {
  let newLeafId = ''

  function doSplit(n: LayoutNode): LayoutNode {
    if (n.type === 'empty') return n
    if (n.type === 'leaf') {
      if (n.id === targetId) {
        newLeafId = crypto.randomUUID()
        return {
          type: 'split',
          id: crypto.randomUUID(),
          orientation,
          children: [n, { type: 'leaf', id: newLeafId, cwd }],
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

  return { node: doSplit(root), newLeafId }
}

export function removeLeaf(root: LayoutNode, targetId: string): LayoutNode {
  if (root.type === 'empty') return root
  if (root.type === 'leaf') {
    return root.id === targetId ? createEmpty() : root
  }

  let removedIndex = -1
  const children: LayoutNode[] = []
  for (let i = 0; i < root.children.length; i++) {
    const child = root.children[i]
    if (child.type === 'leaf' && child.id === targetId) {
      const next = removeLeaf(child, targetId)
      if (next.type === 'empty') {
        removedIndex = i
        continue
      }
      children.push(next)
      continue
    }
    const next = removeLeaf(child, targetId)
    if (next.type === 'empty') continue
    children.push(next)
  }

  if (children.length === 0) return createEmpty()
  if (children.length === 1) return children[0]

  const oldSizes =
    root.sizes && root.sizes.length === root.children.length
      ? root.sizes.slice()
      : root.children.map(() => 100 / root.children.length)

  let newSizes: number[]
  if (removedIndex >= 0) {
    const freed = oldSizes[removedIndex] ?? 0
    newSizes = oldSizes.filter((_, idx) => idx !== removedIndex)
    const neighbor =
      removedIndex === 0
        ? 0
        : removedIndex >= newSizes.length
          ? newSizes.length - 1
          : removedIndex - 1
    if (neighbor >= 0 && neighbor < newSizes.length) {
      newSizes[neighbor] = (newSizes[neighbor] ?? 0) + freed
    }
  } else {
    newSizes = children.map(() => 100 / children.length)
  }

  const total = newSizes.reduce((a, b) => a + b, 0) || 1
  newSizes = newSizes.map((s) => (s / total) * 100)

  return { ...root, children, sizes: newSizes }
}

export function collectLeafIds(root: LayoutNode): string[] {
  if (root.type === 'empty') return []
  if (root.type === 'leaf') return [root.id]
  return root.children.flatMap(collectLeafIds)
}

export function setSplitSizes(
  root: LayoutNode,
  splitId: string,
  sizes: number[],
): LayoutNode {
  if (root.type === 'split') {
    if (root.id === splitId) {
      return { ...root, sizes }
    }
    return { ...root, children: root.children.map((c) => setSplitSizes(c, splitId, sizes)) }
  }
  return root
}

export function getSplitIds(root: LayoutNode): string[] {
  if (root.type === 'split') {
    return [root.id, ...root.children.flatMap(getSplitIds)]
  }
  return []
}

export function countLeaves(root: LayoutNode): number {
  if (root.type === 'empty') return 0
  if (root.type === 'leaf') return 1
  if (!Array.isArray(root.children)) return 0
  return root.children.reduce((sum, c) => sum + countLeaves(c), 0)
}

export function getLeaf(root: LayoutNode, id: string): Extract<LayoutNode, { type: 'leaf' }> | null {
  if (root.type === 'empty') return null
  if (root.type === 'leaf') return root.id === id ? root : null
  for (const c of root.children) {
    const v = getLeaf(c, id)
    if (v !== null) return v
  }
  return null
}

export function getLeafCwd(root: LayoutNode, id: string): string | null {
  if (root.type === 'empty') return null
  if (root.type === 'leaf') return root.id === id ? root.cwd : null
  for (const c of root.children) {
    const v = getLeafCwd(c, id)
    if (v !== null) return v
  }
  return null
}

export function focusAdjacentLeaf(
  root: LayoutNode,
  activeId: string,
  direction: 'left' | 'right',
): string | null {
  const leafIds = collectLeafIds(root)
  if (leafIds.length <= 1) return null

  const index = leafIds.indexOf(activeId)
  if (index < 0) return leafIds[0] ?? null

  if (direction === 'left') {
    return leafIds[(index - 1 + leafIds.length) % leafIds.length]
  }
  return leafIds[(index + 1) % leafIds.length]
}

export interface PaneCycleEntry {
  tabId: string
  paneId: string
}

export function buildPaneCycle(
  tabs: { id: string; layout: LayoutNode }[],
): PaneCycleEntry[] {
  const entries: PaneCycleEntry[] = []
  for (const tab of tabs) {
    for (const paneId of collectLeafIds(tab.layout)) {
      entries.push({ tabId: tab.id, paneId })
    }
  }
  return entries
}

export function cyclePane(
  tabs: { id: string; layout: LayoutNode }[],
  activeTabId: string,
  activePaneId: string,
  direction: 'left' | 'right',
): PaneCycleEntry | null {
  const cycle = buildPaneCycle(tabs)
  if (cycle.length === 0) return null

  const index = cycle.findIndex(
    (e) => e.tabId === activeTabId && e.paneId === activePaneId,
  )
  if (index < 0) return cycle[0] ?? null

  if (direction === 'left') {
    return cycle[(index - 1 + cycle.length) % cycle.length]
  }
  return cycle[(index + 1) % cycle.length]
}

export function findAgentId(node: LayoutNode): string | undefined {
  if (node.type === 'leaf') return node.agentId
  if (node.type === 'split') {
    for (const child of node.children) {
      const id = findAgentId(child)
      if (id) return id
    }
  }
  return undefined
}

export function findAgentLeaves(node: LayoutNode): { id: string; cwd: string; agentBranch?: string; baseBranch?: string }[] {
  if (node.type === 'empty') return []
  if (node.type === 'leaf') {
    if (node.agentBranch && node.cwd) {
      return [{ id: node.id, cwd: node.cwd, agentBranch: node.agentBranch, baseBranch: node.baseBranch }]
    }
    return []
  }
  return node.children.flatMap(findAgentLeaves)
}