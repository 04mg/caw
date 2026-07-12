import { useState } from 'react'
import { ChevronRight, ChevronDown, File, Folder, FolderOpen } from 'lucide-react'
import { type FileNode } from '../types'

interface TreeNodeProps {
  node: FileNode
  depth: number
}

export function TreeNode({ node, depth }: TreeNodeProps) {
  const [expanded, setExpanded] = useState(depth < 1)

  const handleClick = () => {
    if (node.isDir) {
      setExpanded(!expanded)
    }
  }

  return (
    <div>
      <button
        onClick={handleClick}
        className="flex w-full items-center gap-1.5 px-2 py-0.5 text-sm hover:bg-accent/50 text-left"
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        {node.isDir ? (
          <>
            {expanded ? (
              <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            )}
            {expanded ? (
              <FolderOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            ) : (
              <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            )}
          </>
        ) : (
          <>
            <span className="w-7 shrink-0" />
            <File className="h-3.5 w-3.5 shrink-0 text-blue-400" />
          </>
        )}
        <span className="truncate text-muted-foreground">{node.name}</span>
      </button>
      {node.isDir && expanded && node.children && (
        <div>
          {node.children.map((child) => (
            <TreeNode key={child.path} node={child} depth={depth + 1} />
          ))}
          {node.children.length === 0 && (
            <p
              className="text-xs text-muted-foreground px-2 py-1 italic"
              style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}
            >
              empty
            </p>
          )}
        </div>
      )}
    </div>
  )
}
