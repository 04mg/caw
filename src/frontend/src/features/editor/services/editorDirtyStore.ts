import { loader as monacoLoader } from '@monaco-editor/react'

// Track which file paths have unsaved edits, keyed by filePath.
// The actual edited content + undo stack + view state live inside Monaco's
// model (kept alive via `keepCurrentModel`), so we only need a boolean here
// to drive the dirty indicator and the close-confirmation flow.
const dirtyFiles = new Set<string>()

type DirtyListener = (filePath: string) => void
const listeners = new Set<DirtyListener>()

function notify(filePath: string) {
  for (const l of listeners) {
    try { l(filePath) } catch { /* ignore listener errors */ }
  }
}

export function subscribeDirtyChanges(listener: DirtyListener): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function isFileDirty(filePath: string): boolean {
  return dirtyFiles.has(filePath)
}

export function markFileDirty(filePath: string): void {
  if (dirtyFiles.has(filePath)) return
  dirtyFiles.add(filePath)
  notify(filePath)
}

export function clearFileDirty(filePath: string): void {
  if (!dirtyFiles.has(filePath)) return
  dirtyFiles.delete(filePath)
  notify(filePath)
}

// Discard in-memory edits for a file: drop the live Monaco model (so the
// undo stack and edited content are gone) and clear the dirty flag. When the
// file is reopened it will be reloaded from disk.
export function discardFileEdits(filePath: string): void {
  const wasDirty = dirtyFiles.has(filePath)
  dirtyFiles.delete(filePath)
  const monacoInstance = monacoLoader.__getMonacoInstance()
  if (monacoInstance) {
    const uri = monacoInstance.Uri.parse(`file://${filePath}`)
    const model = monacoInstance.editor.getModel(uri)
    if (model) model.dispose()
  }
  if (wasDirty) notify(filePath)
}

// Force-save a file by reading its current Monaco model and PUTing it.
// Resolves true on success, false on failure. No-ops if no model exists.
// Note: this module-level helper relies on the Monaco singleton being
// initialized by an already-mounted Editor; if no Editor has been mounted
// yet, it returns false.
//
// The Monaco model is NOT disposed here, so the editor's undo/redo history
// stays intact after a save (VS Code parity). We only clear the dirty flag.
export async function saveFileFromCache(filePath: string): Promise<boolean> {
  const monacoInstance = monacoLoader.__getMonacoInstance()
  if (!monacoInstance) return false
  const uri = monacoInstance.Uri.parse(`file://${filePath}`)
  const model = monacoInstance.editor.getModel(uri)
  if (!model) return false
  const content = model.getValue()
  try {
    const res = await fetch('/api/workspaces/files', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: filePath, content }),
    })
    if (res.ok) {
      // Mark the saved content as the initial undo checkpoint so Ctrl+Z
      // from a clean state does nothing (matches VS Code), while keeping
      // the full undo stack alive for prior edits.
      try { model.pushStackElement() } catch { /* ignore */ }
      clearFileDirty(filePath)
      return true
    }
    return false
  } catch {
    return false
  }
}