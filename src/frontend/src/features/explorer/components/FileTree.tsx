import { TreeNode } from './TreeNode'
import { type FileNode } from '../types'


export function FileTree({ tree }: { tree: FileNode | null }) {
  if (!tree) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
        No workspace open
      </div>
    )
  }

  return (
    <div className="py-1">
      <TreeNode node={tree} depth={0} />
    </div>
  )
}
