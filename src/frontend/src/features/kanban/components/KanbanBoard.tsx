import { useLayoutEffect, useRef, useEffect, useState } from 'react'
import { 
  Tally1,
  Tally2,
  Tally3,
  Clock, 
  Terminal,
  ChevronRight,
  Workflow
} from 'lucide-react'
import { type Workspace } from '@/features/workspaces/types'
import { collectLeafIds, getLeaf } from '@/features/shared/utils/layout'
import { agentTypes } from '@/features/agents/services/agentTypes'
import { subscribeAgentStatuses, getAgentStatuses, isAgentStatusesHydrated } from '@/features/agents/stores/agentStatusStore'
import { type AgentStatus } from '@/features/agents/types'


interface KanbanBoardProps {
  workspaces: Workspace[]
  onNavigateToWorkspace: (workspaceId: string, tabIndex: number, paneId: string) => void
}

interface WorkspaceDetails {
  workspaceId: string
  workspaceName: string
  workspaceEmoji: string
  tabIndex: number
  paneId: string
  agentBranch?: string
}

type ColumnId = 'idle' | 'needs_input' | 'working'

interface Column {
  id: ColumnId
  title: string
  icon: any
  colorClass: string
  glowClass: string
}

const COLUMNS: Column[] = [
  {
    id: 'idle',
    title: 'Idle',
    icon: Tally1,
    colorClass: 'text-slate-400 border-slate-500/20 bg-slate-500/5',
    glowClass: 'group-hover:border-slate-500/40 group-hover:shadow-[0_0_15px_rgba(148,163,184,0.1)]',
  },
  {
    id: 'working',
    title: 'Working',
    icon: Tally3,
    colorClass: 'text-blue-400 border-blue-500/20 bg-blue-500/5',
    glowClass: 'group-hover:border-blue-500/40 group-hover:shadow-[0_0_15px_rgba(59,130,246,0.15)]',
  },
  {
    id: 'needs_input',
    title: 'Needs Input',
    icon: Tally2,
    colorClass: 'text-amber-400 border-amber-500/20 bg-amber-500/5',
    glowClass: 'group-hover:border-amber-500/40 group-hover:shadow-[0_0_15px_rgba(245,158,11,0.15)]',
  },
]

export function KanbanBoard({ workspaces, onNavigateToWorkspace }: KanbanBoardProps) {
  // Seed from the synchronous store snapshot so the first render already
  // reflects live data — otherwise the subscribe effect only fires after
  // paint, flashing "No agents in ..." placeholders for one frame.
  const [statuses, setStatuses] = useState<Record<string, AgentStatus>>(() => getAgentStatuses())
  const [hydrated, setHydrated] = useState<boolean>(() => isAgentStatusesHydrated())
  const cardsRef = useRef<Record<string, DOMRect>>({})
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768)

  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  // Subscribe to the background WS status store. The store is kept live for
  // the whole app lifetime (the multiplexer onSubscribe dumps the current
  // snapshot), so opening the Command Center is instant — no REST fetch.
  useEffect(() => {
    return subscribeAgentStatuses((nextStatuses) => {
      setStatuses(nextStatuses)
      setHydrated(isAgentStatusesHydrated())
    })
  }, [])

  // FLIP Layout Animation
  useLayoutEffect(() => {
    const newPositions: Record<string, DOMRect> = {}
    const elements = document.querySelectorAll('[data-card-id]')

    elements.forEach((el) => {
      const id = el.getAttribute('data-card-id')
      if (id) {
        newPositions[id] = el.getBoundingClientRect()
      }
    })

    Object.keys(newPositions).forEach((id) => {
      const first = cardsRef.current[id]
      const last = newPositions[id]

      if (first && last) {
        const dx = first.left - last.left
        const dy = first.top - last.top

        if (dx !== 0 || dy !== 0) {
          const el = document.querySelector(`[data-card-id="${id}"]`) as HTMLElement
          if (el) {
            el.style.transform = `translate(${dx}px, ${dy}px)`
            el.style.transition = 'none'

            requestAnimationFrame(() => {
              el.style.transform = ''
              el.style.transition = 'transform 0.4s cubic-bezier(0.25, 0.8, 0.25, 1)'
            })
          }
        }
      }
    })

    cardsRef.current = newPositions
  }, [statuses])

  // Resolve a card to the workspace pane it should open.
  // Prefer the exact session id, then fall back to matching the agent cwd
  // against the leaf cwd so cards still navigate when the leaf mapping has
  // drifted.
  const findWorkspaceDetails = (agent: AgentStatus): WorkspaceDetails | null => {
    const resolveBySessionId = (): WorkspaceDetails | null => {
      for (const ws of workspaces) {
        for (let tabIdx = 0; tabIdx < ws.layouts.length; tabIdx++) {
          const tab = ws.layouts[tabIdx]
          const leafIds = collectLeafIds(tab.layout)
          if (leafIds.includes(agent.sessionId)) {
            const leaf = getLeaf(tab.layout, agent.sessionId)
            return {
              workspaceId: ws.id,
              workspaceName: ws.name || ws.path || 'Workspace',
              workspaceEmoji: ws.emoji || '💼',
              tabIndex: tabIdx,
              paneId: agent.sessionId,
              agentBranch: leaf?.agentBranch,
            }
          }
        }
      }
      return null
    }

    const bySession = resolveBySessionId()
    if (bySession) return bySession

    if (!agent.cwd) return null

    for (const ws of workspaces) {
      for (let tabIdx = 0; tabIdx < ws.layouts.length; tabIdx++) {
        const tab = ws.layouts[tabIdx]
        const leafIds = collectLeafIds(tab.layout)
        for (const leafId of leafIds) {
          const leaf = getLeaf(tab.layout, leafId)
          if (!leaf || leaf.cwd !== agent.cwd) continue
          return {
            workspaceId: ws.id,
            workspaceName: ws.name || ws.path || 'Workspace',
            workspaceEmoji: ws.emoji || '💼',
            tabIndex: tabIdx,
            paneId: leafId,
            agentBranch: leaf?.agentBranch,
          }
        }
      }
    }
    return null
  }

  // Map AgentStatus status to ColumnId
  const getColumnForStatus = (statusStr: string): ColumnId => {
    const status = statusStr.toLowerCase()
    if (status === 'thinking' || status === 'executing') {
      return 'working'
    }
    if (status === 'waiting_input') {
      return 'needs_input'
    }
    return 'idle'
  }

  // Format relative timestamp
  const formatTime = (isoString: string) => {
    try {
      const date = new Date(isoString)
      const diffMs = Date.now() - date.getTime()
      const diffSecs = Math.floor(diffMs / 1000)
      const diffMins = Math.floor(diffSecs / 60)
      const diffHrs = Math.floor(diffMins / 60)

      if (diffSecs < 10) return 'just now'
      if (diffSecs < 60) return `${diffSecs}s ago`
      if (diffMins < 60) return `${diffMins}m ago`
      if (diffHrs < 24) return `${diffHrs}h ago`
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  // Group agent statuses by Column
  const groupedAgents: Record<ColumnId, AgentStatus[]> = {
    idle: [],
    needs_input: [],
    working: [],
  }

  Object.values(statuses).forEach((agent) => {
    const colId = getColumnForStatus(agent.status)
    groupedAgents[colId].push(agent)
  })

  // Keep a stable ordering based on the backend-assigned opening sequence
  // (falling back to timestamp) so the cards don't reshuffle every time the
  // Control Center is reopened and the list is re-fetched.
  const byStableOrder = (a: AgentStatus, b: AgentStatus) => {
    const sa = typeof a.sequence === 'number' ? a.sequence : Number.MAX_SAFE_INTEGER
    const sb = typeof b.sequence === 'number' ? b.sequence : Number.MAX_SAFE_INTEGER
    if (sa !== sb) return sa - sb
    return new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  }
  ;(Object.keys(groupedAgents) as ColumnId[]).forEach((colId) => {
    groupedAgents[colId].sort(byStableOrder)
  })

  // Render a single Agent Card
  const renderCard = (agent: AgentStatus) => {
    const wsDetails = findWorkspaceDetails(agent)
    const agentDef = agentTypes[agent.agentId]
    const AgentIcon = agentDef?.icon || Terminal
    const label = agentDef?.label || agent.agentId

    // Choose column classes for styling card headers/borders
    const colId = getColumnForStatus(agent.status)
    const colConf = COLUMNS.find(c => c.id === colId)

    const handleCardClick = () => {
      if (wsDetails) {
        onNavigateToWorkspace(wsDetails.workspaceId, wsDetails.tabIndex, wsDetails.paneId)
      }
    }

    return (
      <div
        key={agent.sessionId}
        data-card-id={agent.sessionId}
        data-testid="kanban-card"
        onClick={handleCardClick}
        className={`group relative overflow-hidden cursor-pointer rounded-xl border border-border/50 bg-secondary/15 backdrop-blur-md p-4 transition-all duration-300 active:scale-[0.98] select-none flex flex-col gap-3.5 shadow-sm hover:shadow-md hover:bg-secondary/25 ${colConf?.glowClass || ''}`}
      >
        {/* Large semi-transparent background brand logo watermark */}
        <div className="absolute right-[-15px] bottom-[-15px] opacity-[0.03] group-hover:opacity-[0.07] transition-opacity duration-300 pointer-events-none select-none">
          <AgentIcon className="w-24 h-24 rotate-[15deg] text-foreground" />
        </div>

        {/* Card Header */}
        <div className="flex items-center justify-between z-10">
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-lg bg-background/50 border border-border/40 group-hover:border-foreground/20 transition-colors">
              <AgentIcon className="w-5 h-5 text-foreground" />
            </div>
            <span className="font-semibold text-sm text-foreground/90 group-hover:text-foreground transition-colors">
              {label}
            </span>
          </div>
          
          {/* Status Indicator Dot/Badge */}
          <div className="flex items-center gap-1.5">
            <span className={`relative flex h-2 w-2`}>
              {colId === 'working' && (
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span>
              )}
              {colId === 'needs_input' && (
                <span className="animate-pulse absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"></span>
              )}
              <span className={`relative inline-flex rounded-full h-2 w-2 ${
                colId === 'working' ? 'bg-blue-400' : colId === 'needs_input' ? 'bg-amber-400' : 'bg-slate-400'
              }`}></span>
            </span>
          </div>
        </div>

        {/* Card Content - Dynamic Description */}
        <div className="flex flex-col gap-2 z-10">
          {/* Title / Session Name */}
          <div className="text-xs font-medium bg-accent/15 rounded-lg p-2.5 border border-border/30 italic group-hover:bg-accent/25 transition-colors h-[36px] overflow-hidden flex items-center">
            <div
              className="whitespace-nowrap overflow-hidden w-full"
              style={{
                maskImage: 'linear-gradient(to right, black 85%, transparent 100%)',
                WebkitMaskImage: 'linear-gradient(to right, black 85%, transparent 100%)'
              }}
            >
              {agent.title ? (
                <span className="text-foreground/90">{agent.title}</span>
              ) : (
                <span className="text-muted-foreground">Unnamed Session</span>
              )}
            </div>
          </div>

          {/* Active Tool / Details */}
          {(agent.tool || agent.details) && (
            <div className="flex flex-col gap-1 mt-1 text-[11px] font-mono text-muted-foreground/90">
              {agent.tool && (
                <div className="flex items-center gap-1">
                  <span className="text-primary/70 shrink-0 font-sans text-[10px] uppercase tracking-wider">Tool:</span>
                  <span className="bg-background/80 px-1.5 py-0.5 rounded border border-border/30 text-foreground/80 truncate">
                    {agent.tool}
                  </span>
                </div>
              )}
              {agent.details && (
                <div className="flex items-start gap-1 mt-0.5">
                  <span className="text-primary/70 shrink-0 font-sans text-[10px] uppercase tracking-wider">Info:</span>
                  <span className="text-foreground/70 truncate max-w-[250px]" title={agent.details}>
                    {agent.details}
                  </span>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Card Footer - Workspace and Clock */}
        <div className="flex items-center justify-between border-t border-border/20 pt-2.5 text-[11px] text-muted-foreground z-10">
          {wsDetails ? (
            <div className="flex items-center gap-1.5 hover:text-foreground transition-colors max-w-[70%]">
              <span className="truncate">
                {wsDetails.workspaceEmoji} {wsDetails.workspaceName}
              </span>
              {wsDetails.agentBranch && (
                <>
                  <span className="text-border select-none">·</span>
                  <span className="flex items-center gap-1 text-foreground/70 shrink-0">
                    <Workflow className="w-3 h-3 text-violet-400" />
                    <span className="font-mono text-[10px] truncate max-w-[120px]">{wsDetails.agentBranch}</span>
                  </span>
                </>
              )}
            </div>
          ) : (
            <span className="text-muted-foreground/50 italic">Unknown Workspace</span>
          )}

          <div className="flex items-center gap-1 text-[10px] shrink-0">
            <Clock className="w-3 h-3 opacity-60" />
            <span>{formatTime(agent.timestamp)}</span>
          </div>
        </div>

        {/* Hover Arrow Overlay */}
        <div className="absolute right-0 top-0 opacity-0 group-hover:opacity-100 transition-opacity duration-300 pl-4 bg-gradient-to-l from-secondary/80 to-transparent h-full flex items-center justify-end pointer-events-none w-12 rounded-r-xl">
          <ChevronRight className="w-4 h-4 text-foreground/80 mr-3 translate-x-2 group-hover:translate-x-0 transition-transform duration-300" />
        </div>
      </div>
    )
  }

  // Define mobile columns (Idle -> Working -> Needs Input, same as desktop)
  const MOBILE_COLUMNS: Column[] = [
    {
      id: 'idle',
      title: 'Idle',
      icon: Tally1,
      colorClass: 'text-slate-400 border-slate-500/20 bg-slate-500/5',
      glowClass: 'group-hover:border-slate-500/40 group-hover:shadow-[0_0_15px_rgba(148,163,184,0.1)]',
    },
    {
      id: 'working',
      title: 'Working',
      icon: Tally3,
      colorClass: 'text-blue-400 border-blue-500/20 bg-blue-500/5',
      glowClass: 'group-hover:border-blue-500/40 group-hover:shadow-[0_0_15px_rgba(59,130,246,0.15)]',
    },
    {
      id: 'needs_input',
      title: 'Needs Input',
      icon: Tally2,
      colorClass: 'text-amber-400 border-amber-500/20 bg-amber-500/5',
      glowClass: 'group-hover:border-amber-500/40 group-hover:shadow-[0_0_15px_rgba(245,158,11,0.15)]',
    },
  ]

  if (isMobile) {
    const totalAgents = groupedAgents.idle.length + groupedAgents.working.length + groupedAgents.needs_input.length
    if (totalAgents === 0 && hydrated) {
      return (
        <div className="flex flex-col h-full w-full items-center justify-center px-6 text-center gap-4 select-none">
          <div className="p-4 rounded-full bg-muted/30 border border-border/30">
            <Workflow className="w-8 h-8 text-muted-foreground" />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-sm font-semibold text-foreground/80">No agents running</span>
            <span className="text-xs text-muted-foreground/60">
              Start a new agent or terminal from the Terminals tab.
            </span>
          </div>
        </div>
      )
    }
    return (
        <div data-testid="kanban-board" className="flex flex-col h-full w-full overflow-y-auto p-4 gap-6 scrollbar-thin">
        {MOBILE_COLUMNS.map((col) => {
          const agents = groupedAgents[col.id]

          const ColIcon = col.icon
          return (
            <div key={col.id} data-testid={`kanban-column-${col.id}`} className="flex flex-col shrink-0 gap-3 rounded-xl p-3 bg-secondary/5">
              <div className="flex items-center justify-between pb-2 border-b border-border/20">
                <div className="flex items-center gap-2">
                  <ColIcon className="w-4 h-4 text-foreground" />
                  <span className="text-xs font-bold uppercase tracking-wider text-foreground/90">
                    {col.title}
                  </span>
                </div>
                <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-accent/40 text-muted-foreground border border-border/30 font-mono">
                  {agents.length}
                </span>
              </div>
              <div className="flex flex-col gap-2">
                {agents.length > 0 ? (
                  agents.map(renderCard)
                ) : hydrated ? (
                  <div className="flex flex-col items-center justify-center border border-dashed border-border/20 rounded-xl p-4 text-center text-xs text-muted-foreground/60 italic gap-2 min-h-[60px]">
                    <div className="p-2 rounded-full bg-muted/40">
                      <ColIcon className="w-3.5 h-3.5 text-foreground" />
                    </div>
                    <span>No agents in {col.title}</span>
                  </div>
                ) : null}
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div data-testid="kanban-board" className="flex flex-col h-full w-full overflow-hidden p-6 gap-6">
      {/* Kanban Board Columns */}
      <div className="flex-1 min-h-0 flex gap-4 overflow-x-auto pb-2">
        {COLUMNS.map((col) => {
          const agents = groupedAgents[col.id]
          const ColIcon = col.icon

          return (
            <div 
              key={col.id}
              data-testid={`kanban-column-${col.id}`}
              className="flex flex-col min-w-[280px] flex-1 rounded-xl p-4 min-h-[400px]"
            >
              {/* Column Header */}
              <div className="flex items-center justify-between pb-3.5 border-b border-border/40 mb-4 shrink-0">
                <div className="flex items-center gap-2">
                  <ColIcon className="w-4 h-4 text-foreground" />
                  <span className="text-xs font-bold tracking-wider uppercase text-foreground/90">
                    {col.title}
                  </span>
                </div>
                <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-accent/40 text-muted-foreground border border-border/30 font-mono">
                  {agents.length}
                </span>
              </div>

              {/* Card List */}
              <div className="flex-1 overflow-y-auto pr-0.5 space-y-1.5 scrollbar-thin">
                {agents.length > 0 ? (
                  agents.map(renderCard)
                ) : hydrated ? (
                  <div className="h-full flex flex-col items-center justify-center border border-dashed border-border/20 rounded-xl p-6 text-center text-xs text-muted-foreground/60 italic gap-2 min-h-[150px]">
                    <div className="p-2.5 rounded-full bg-muted/40">
                      <ColIcon className="w-4 h-4 text-foreground" />
                    </div>
                    <span>No agents in {col.title}</span>
                  </div>
                ) : null}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
