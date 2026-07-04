export type LayoutNode =
  | { type: 'leaf'; id: string; cwd: string }
  | { type: 'split'; id: string; orientation: 'horizontal' | 'vertical'; children: LayoutNode[]; sizes: number[] }
  | { type: 'empty' }

export function createEmpty(): LayoutNode {
  return { type: 'empty' }
}

export function createLeaf(cwd: string): LayoutNode {
  return { type: 'leaf', id: crypto.randomUUID(), cwd }
}

export function splitLeaf(
  root: LayoutNode,
  targetId: string,
  orientation: 'horizontal' | 'vertical',
  cwd: string,
): LayoutNode {
  if (root.type === 'empty') return root
  if (root.type === 'leaf') {
    if (root.id === targetId) {
      return {
        type: 'split',
        id: crypto.randomUUID(),
        orientation,
        children: [root, createLeaf(cwd)],
        sizes: [50, 50],
      }
    }
    return root
  }
  return {
    ...root,
    children: root.children.map((c) => splitLeaf(c, targetId, orientation, cwd)),
  }
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
  return root.children.reduce((sum, c) => sum + countLeaves(c), 0)
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