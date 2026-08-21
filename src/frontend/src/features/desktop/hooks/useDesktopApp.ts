import { useSyncExternalStore } from 'react'
import { getDesktopApps, subscribePrefs, type DesktopAppPref } from '@/features/prefs/stores/prefsStore'

// Resolves the icon config for a desktop app by its id (the leaf's agentId).
// Desktop tabs only carry the app id, so tab buttons join against the prefs
// cache; subscribing keeps them in sync when the user edits icons in
// Settings. Returns undefined when the id is not a desktop app.
export function useDesktopApp(appId: string | undefined): DesktopAppPref | undefined {
  const apps = useSyncExternalStore(subscribePrefs, getDesktopApps)
  if (!appId) return undefined
  return apps.find((a) => a.id === appId)
}
