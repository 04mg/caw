import { loader as monacoLoader } from '@monaco-editor/react'

// Track which file paths have unsaved edits, keyed by filePath.
// The actual edited content + undo stack + view state live inside Monaco's
// model (kept alive via `keepCurrentModel`), so we only need a boolean here
// to drive the dirty indicator and the close-confirmation flow.
const dirtyFiles = new Set<string>()

export function isFileDirty(filePath: string): boolean {
  return dirtyFiles.has(filePath)
}

export function markFileDirty(filePath: string): void {
  dirtyFiles.add(filePath)
}

export function clearFileDirty(filePath: string): void {
  dirtyFiles.delete(filePath)
}

// Discard in-memory edits for a file: drop the live Monaco model (so the
// undo stack and edited content are gone) and clear the dirty flag. When the
// file is reopened it will be reloaded from disk.
export function discardFileEdits(filePath: string): void {
  dirtyFiles.delete(filePath)
  const monacoInstance = monacoLoader.__getMonacoInstance()
  if (monacoInstance) {
    const uri = monacoInstance.Uri.parse(`file://${filePath}`)
    const model = monacoInstance.editor.getModel(uri)
    if (model) model.dispose()
  }
}

// Force-save a file by reading its current Monaco model and PUTing it.
// Resolves true on success, false on failure. No-ops if no model exists.
// Note: this module-level helper relies on the Monaco singleton being
// initialized by an already-mounted Editor; if no Editor has been mounted
// yet, it returns false.
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
      dirtyFiles.delete(filePath)
      return true
    }
    return false
  } catch {
    return false
  }
}