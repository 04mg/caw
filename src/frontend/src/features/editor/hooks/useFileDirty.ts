import { useSyncExternalStore, useCallback } from 'react'
import { isFileDirty, subscribeDirtyChanges } from '../services/editorDirtyStore'

// Reactively tracks the dirty state of a single file path. Re-renders the
// calling component whenever the file's dirty flag changes (edit, save,
// discard, reload). Avoids polling: the editorDirtyStore pushes updates.
export function useFileDirty(filePath?: string): boolean {
  const subscribe = useCallback((onStoreChange: () => void) => {
    const unsub = subscribeDirtyChanges((changed) => {
      if (!filePath || changed === filePath) onStoreChange()
    })
    return unsub
  }, [filePath])

  const getSnapshot = useCallback(() => {
    return filePath ? isFileDirty(filePath) : false
  }, [filePath])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}