import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  appendTypeahead,
  findTypeaheadIndex,
  stepIndex,
} from './folderTypeahead'

const items = [
  { name: 'Applications', path: '/Applications' },
  { name: 'Documents', path: '/Users/u/Documents' },
  { name: 'Downloads', path: '/Users/u/Downloads' },
  { name: 'Desktop', path: '/Users/u/Desktop' },
  { name: 'project', path: '/Users/u/Documents/project' },
  { name: 'project_bak', path: '/Users/u/Documents/project_bak' },
  { name: 'evm_cuda', path: '/Users/u/Documents/projects/evm_cuda' },
]

test('findTypeaheadIndex: stays on current when multi-char query extends and still matches', () => {
  // Documents (1) and Downloads (2) both start with 'do'.
  assert.equal(findTypeaheadIndex(items, 'do', 1), 1)
  // 'dow' narrows: Documents no longer prefix-matches → advance to Downloads.
  assert.equal(findTypeaheadIndex(items, 'dow', 1), 2)
})

test('findTypeaheadIndex: does not bounce between same-prefix siblings', () => {
  // project (4) and project_bak (5) share prefix 'project'. Typing the
  // shared prefix one char at a time must STAY on project, not alternate.
  assert.equal(findTypeaheadIndex(items, 'pr', 4), 4)
  assert.equal(findTypeaheadIndex(items, 'pro', 4), 4)
  assert.equal(findTypeaheadIndex(items, 'proj', 4), 4)
  assert.equal(findTypeaheadIndex(items, 'projec', 4), 4)
  assert.equal(findTypeaheadIndex(items, 'project', 4), 4)
  // Beyond the shared prefix, advance to project_bak.
  assert.equal(findTypeaheadIndex(items, 'project_', 4), 5)
  assert.equal(findTypeaheadIndex(items, 'project_b', 4), 5)
  assert.equal(findTypeaheadIndex(items, 'project_bak', 4), 5)
})

test('findTypeaheadIndex: single-char query cycles among prefix matches', () => {
  // Single 'd' scans forward (does NOT pin to current).
  assert.equal(findTypeaheadIndex(items, 'd', 1), 2) // Documents → Downloads
  assert.equal(findTypeaheadIndex(items, 'd', 2), 3) // Downloads → Desktop
  assert.equal(findTypeaheadIndex(items, 'd', 3), 1) // Desktop → Documents (wrap)
})

test('findTypeaheadIndex: substring fallback when no prefix', () => {
  assert.equal(findTypeaheadIndex(items, 'cuda', -1), 6)
})

test('findTypeaheadIndex: no match returns -1', () => {
  assert.equal(findTypeaheadIndex(items, 'zzz', -1), -1)
})

test('findTypeaheadIndex: empty query or empty list', () => {
  assert.equal(findTypeaheadIndex(items, '', 0), -1)
  assert.equal(findTypeaheadIndex([], 'd', 0), -1)
})

test('stepIndex: wraps at ends', () => {
  assert.equal(stepIndex(0, 7, -1), 6)
  assert.equal(stepIndex(6, 7, 1), 0)
})

test('stepIndex: from unselected goes to edge', () => {
  assert.equal(stepIndex(-1, 7, 1), 0)
  assert.equal(stepIndex(-1, 7, -1), 6)
})

test('appendTypeahead: lowercases and appends', () => {
  assert.equal(appendTypeahead('', 'D'), 'd')
  assert.equal(appendTypeahead('d', 'o'), 'do')
})
