import { useEffect } from 'react'
import { AppLayout } from '@/components/AppLayout'
import { TooltipProvider } from '@/components/ui/tooltip'

export default function App() {
  useEffect(() => {
    const handleContextMenu = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (!target) return

      // Whitelist selector matches:
      // 1. Monaco Editor (main editor, context views, menus, etc.)
      const isMonaco =
        target.closest('.monaco-editor') ||
        target.closest('.context-view') ||
        target.closest('.monaco-menu-container') ||
        target.closest('.monaco-menu')

      // 2. Explorer (FolderSidebar panel and its custom context menus)
      const isExplorer =
        target.closest('.explorer-sidebar') ||
        target.closest('.smart-context-menu')

      // 3. Workspace Panel (WorkspacePanel panel and its context menus)
      const isWorkspace =
        target.closest('.workspace-panel') ||
        target.closest('.workspace-context-menu')

      // 4. Any other custom menus or future components explicitly opting in
      const isCustomMenu =
        target.closest('.custom-context-menu') ||
        target.closest('[data-allow-context-menu="true"]')

      if (isMonaco || isExplorer || isWorkspace || isCustomMenu) {
        return // Allow normal/custom context menu
      }

      // Block standard browser right-click context menu elsewhere
      e.preventDefault()
    }

    document.addEventListener('contextmenu', handleContextMenu)
    return () => {
      document.removeEventListener('contextmenu', handleContextMenu)
    }
  }, [])

  return (
    <TooltipProvider>
      <AppLayout />
    </TooltipProvider>
  )
}
