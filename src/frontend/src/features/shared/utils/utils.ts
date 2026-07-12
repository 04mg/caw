import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'
import { type LayoutNode } from './layout'
import { type Workspace, type TabLayout } from '@/features/workspaces/types'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

function layoutEqual(a: LayoutNode, b: LayoutNode): boolean {
  if (a.type !== b.type) return false
  if (a.type === 'empty' && b.type === 'empty') return true
  if (a.type === 'leaf' && b.type === 'leaf') {
    if (a.id !== b.id) return false
    if (a.cwd !== b.cwd) return false
    if (a.agentId !== b.agentId) return false
    if (a.filePath !== b.filePath) return false
    if (a.isDiff !== b.isDiff) return false
    if (a.agentBranch !== b.agentBranch) return false
    if (a.baseBranch !== b.baseBranch) return false
    const ac = a.cmd, bc = b.cmd
    if (ac !== bc) {
      if (!ac || !bc || ac.length !== bc.length) return false
      for (let i = 0; i < ac.length; i++) if (ac[i] !== bc[i]) return false
    }
    return true
  }
  if (a.type === 'split' && b.type === 'split') {
    if (a.id !== b.id) return false
    if (a.orientation !== b.orientation) return false
    if (a.children.length !== b.children.length) return false
    const asz = a.sizes, bsz = b.sizes
    if (asz && bsz) {
      if (asz.length !== bsz.length) return false
      for (let i = 0; i < asz.length; i++) if (asz[i] !== bsz[i]) return false
    }
    for (let i = 0; i < a.children.length; i++) {
      if (!layoutEqual(a.children[i], b.children[i])) return false
    }
    return true
  }
  return false
}

function tabLayoutEqual(a: TabLayout, b: TabLayout): boolean {
  if (a.id !== b.id) return false
  if (a.name !== b.name) return false
  return layoutEqual(a.layout, b.layout)
}

export function workspacesEqual(a: Workspace[], b: Workspace[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    const aw = a[i], bw = b[i]
    if (aw.id !== bw.id) return false
    if (aw.path !== bw.path) return false
    if (aw.name !== bw.name) return false
    if (aw.emoji !== bw.emoji) return false
    if (aw.enableWorktrees !== bw.enableWorktrees) return false
    if (aw.layouts.length !== bw.layouts.length) return false
    for (let j = 0; j < aw.layouts.length; j++) {
      if (!tabLayoutEqual(aw.layouts[j], bw.layouts[j])) return false
    }
  }
  return true
}
