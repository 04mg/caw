import { useEffect, useMemo, useRef, useState } from 'react'
import { type AgentStatus } from '@/features/agents/types'
import { getPetState } from '../petStates'
import { type PetEntry } from '../petsStore'
import { PET_STRIP_HEIGHT } from '../petAssignment'

// Base sprite state driven by the agent's live status. Persisting states
// stay active as long as the status holds: failed for any failure, jumping
// while the agent awaits input, review while working, idle once finished.
const STATUS_STATE: Record<string, string> = {
  thinking: 'review',
  executing: 'review',
  waiting_input: 'jumping',
  idle: 'idle',
  crashed: 'failed',
  tool_failed: 'failed',
  interrupted: 'failed',
}

function baseStateFromStatus(status: AgentStatus | undefined): string {
  if (!status) return 'idle'
  return STATUS_STATE[status.status] ?? 'idle'
}

function isWorking(status: AgentStatus | undefined): boolean {
  const s = status?.status
  return s === 'thinking' || s === 'executing'
}

function hashString(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0
  }
  return Math.abs(h)
}

const PAD = 8
const WALK_GAP = 6
const MIN_SCALE = 0.42
const MAX_SCALE = 0.8
const FALL_SPEED = 85

export interface PetRange {
  x: number
  w: number
}

interface PetProps {
  pet: PetEntry
  leafId: string
  status: AgentStatus | undefined
  x?: number
  y?: number
  containerW: number
  containerH: number
  getOtherRanges: () => PetRange[]
  onPose: (pose: PetRange) => void
  onClick: () => void
}

interface PetState {
  x: number
  y: number
  groundY: number
  targetX: number
  dir: 1 | -1
  walking: boolean
  decisionAt: number
  lastTick: number
  flashState: string | null
  flashUntil: number
  spriteState: string
  spriteStarted: number
  status: AgentStatus | undefined
  hadStatus: boolean
}

export function Pet({ pet, leafId, status, x = 0, y = 0, containerW, containerH, getOtherRanges, onPose, onClick }: PetProps) {
  const elRef = useRef<HTMLDivElement | null>(null)
  const [size, setSize] = useState<{ petW: number; petH: number; rows: number } | null>(null)
  const stateRef = useRef<PetState | null>(null)
  const propsRef = useRef({ containerW, containerH })
  propsRef.current = { containerW, containerH }
  // Live status is read from a ref so the animation loop and the status
  // effect can see the latest value without restarting the loop.
  const statusRef = useRef<AgentStatus | undefined>(status)
  statusRef.current = status
  // "Finished" bubble: shown once the agent has finished its first run,
  // dismissed on click.
  const [showBubble, setShowBubble] = useState(false)
  const bubbleDismissedRef = useRef(false)
  // Tracks whether the agent has ever left idle, so a freshly opened agent
  // (which sits idle from the start) is never greeted with "Finished".
  const startedWorkRef = useRef(false)
  // Pointer drag state: hold and move the pet anywhere (even vertically);
  // on release it slowly falls back to its ground position.
  const draggingRef = useRef(false)
  const dragMovedRef = useRef(false)
  const dragPointerIdRef = useRef<number | null>(null)
  const dragLastRef = useRef({ x: 0, y: 0 })
  // Pane origin is read from a ref so pane moves (splitter drags) apply to
  // the running animation loop without restarting it.
  const originRef = useRef({ x, y })
  originRef.current = { x, y }
  const apiRef = useRef({ getOtherRanges, onPose })
  apiRef.current = { getOtherRanges, onPose }

  // Rounded to two decimals so small pane resize deltas don't restart the
  // preload effect (and with it the animation loop) on every drag tick.
  const scale = useMemo(() => {
    if (containerH <= 0) return MIN_SCALE
    const s = Math.min(MAX_SCALE, Math.max(MIN_SCALE, containerH / 640))
    return Math.round(s * 100) / 100
  }, [containerH])

  // Preload the spritesheet so the pet appears only once it can render.
  useEffect(() => {
    let cancelled = false
    const img = new Image()
    img.onload = () => {
      if (cancelled) return
      const w = img.naturalWidth || 192 * 8
      const h = img.naturalHeight || 208 * 9
      const rows = Math.round((h * 1536) / (208 * w)) || 9
      // The pet floats on the bottom strip over the terminals, so cap its
      // height to keep it inside that strip.
      const petH = Math.round(Math.min(208 * scale, PET_STRIP_HEIGHT))
      const petW = Math.round((petH * 192) / 208)
      setSize({ petW, petH, rows })
    }
    img.src = pet.spritesheetUrl
    return () => {
      cancelled = true
      img.onload = null
      img.src = ''
    }
  }, [pet.spritesheetUrl, scale])

  // Handle status transitions. A freshly spawned agent waves once; the
  // "Finished" bubble follows the first completed run until the pet is
  // clicked.
  useEffect(() => {
    const st = stateRef.current
    if (st) st.status = status

    const isIdle = status?.status === 'idle'
    if (status && status.status !== 'idle') {
      startedWorkRef.current = true
    }
    if (isIdle) {
      if (startedWorkRef.current && !bubbleDismissedRef.current) setShowBubble(true)
    } else {
      bubbleDismissedRef.current = false
      setShowBubble(false)
    }

    if (!st || !status) return
    if (!st.hadStatus) {
      st.hadStatus = true
      st.flashState = 'waving'
      st.flashUntil = performance.now() + 900
    }
  }, [status])

  // Main animation loop: movement + sprite frame stepping, driven
  // imperatively against the DOM to avoid per-frame React re-renders.
  useEffect(() => {
    if (!size) return
    const el = elRef.current
    if (!el) return
    const { petW, petH, rows } = size
    let st = stateRef.current
    let created = false
    if (!st) {
      st = {
        x: PAD,
        y: 0,
        groundY: 0,
        targetX: PAD,
        dir: 1,
        walking: false,
        decisionAt: 0,
        lastTick: 0,
        flashState: null,
        flashUntil: 0,
        spriteState: 'idle',
        spriteStarted: 0,
        status: undefined,
        hadStatus: false,
      }
      stateRef.current = st
      created = true
    }
    // The pet appears for an agent that is already running: welcome it with
    // a wave, just like a fresh spawn.
    if (created && !st.hadStatus && statusRef.current) {
      st.hadStatus = true
      st.flashState = 'waving'
      st.flashUntil = performance.now() + 900
    }

    const { containerW, containerH } = propsRef.current
    const max = Math.max(PAD, containerW - petW - PAD)
    if (st.lastTick === 0) {
      const x0 = PAD + (hashString(leafId + pet.slug + 'pos') % Math.max(1, Math.round(max - PAD)))
      st.x = Math.min(max, Math.max(PAD, x0))
      st.targetX = st.x
    }
    st.groundY = containerH - petH
    if (created) st.y = st.groundY
    st.lastTick = performance.now()

    el.style.backgroundImage = `url(${pet.spritesheetUrl})`
    el.style.backgroundSize = `${8 * petW}px ${rows * petH}px`
    el.style.width = `${petW}px`
    el.style.height = `${petH}px`
    el.style.imageRendering = 'pixelated'

    const applyPose = () => {
      const { x: ox, y: oy } = originRef.current
      el.style.transform = `translate3d(${ox + st.x}px, ${oy + st.y}px, 0)`
    }

    // Movement decisions for a working agent: keep pacing left-to-right,
    // pausing for a review between segments, and hop back to the left edge
    // once it reaches the right one.
    const decide = (now: number) => {
      const { containerW } = propsRef.current
      const maxX = Math.max(PAD, containerW - petW - PAD)
      if (st.x >= maxX) {
        st.x = PAD
        st.decisionAt = now + 500
        return
      }
      const step = Math.min(90 + Math.random() * 200, Math.max(0, maxX - st.x))
      const others = apiRef.current.getOtherRanges()
      const candidate = st.x + step
      const blocked = others.some(
        (r) => candidate < r.x + r.w + WALK_GAP && candidate + petW + WALK_GAP > r.x,
      )
      if (blocked) {
        st.decisionAt = now + 1000 + Math.random() * 1000
        return
      }
      st.targetX = candidate
      st.dir = 1
      st.walking = true
      st.decisionAt = now + 30000
    }

    let raf = 0
    const tick = (now: number) => {
      const { containerW } = propsRef.current
      const maxX = Math.max(PAD, containerW - petW - PAD)
      const dt = Math.min((now - st.lastTick) / 1000, 0.1)
      st.lastTick = now

      const working = isWorking(st.status)
      // Idle / failed / awaiting-input pets stay put and spam their state.
      if (!working) st.walking = false

      // A pet dropped in mid-air slowly falls back to its ground position.
      st.groundY = containerH - petH
      if (!draggingRef.current && st.y < st.groundY) {
        st.y = Math.min(st.groundY, st.y + FALL_SPEED * dt)
      }
      if (draggingRef.current) st.walking = false

      if (st.walking) {
        const speed = 90 * dt
        if (Math.abs(st.targetX - st.x) <= speed) {
          st.x = st.targetX
          st.walking = false
          st.decisionAt = now + 600 + Math.random() * 1400
        } else {
          st.x += st.dir * speed
          if (st.dir > 0 && st.x >= maxX) {
            // Walked off the right edge: reappear from the left and keep going.
            st.x = PAD
            st.targetX = PAD
            st.walking = false
            st.decisionAt = now + 700 + Math.random() * 1200
          } else if (st.x <= PAD) {
            st.x = PAD
            st.walking = false
            st.decisionAt = now + 1200 + Math.random() * 1200
          }
        }
      } else if (working && now >= st.decisionAt && !draggingRef.current) {
        decide(now)
      }

      // Resolve the sprite state: flashes win while active, walking uses the
      // running rows, otherwise the status base state.
      let state: string
      if (st.flashState && now < st.flashUntil) {
        state = st.flashState
      } else if (st.walking) {
        state = st.dir > 0 ? 'running-right' : 'running-left'
      } else {
        state = baseStateFromStatus(st.status)
      }
      if (state !== st.spriteState) {
        st.spriteState = state
        st.spriteStarted = now
      }
      const def = getPetState(state)
      const elapsed = (now - st.spriteStarted) % def.duration
      const frame = Math.min(def.frames - 1, Math.floor((elapsed / def.duration) * def.frames))
      el.style.backgroundPosition = `-${frame * petW}px -${def.row * petH}px`

      applyPose()
      apiRef.current.onPose({ x: st.x, w: petW })
      raf = requestAnimationFrame(tick)
    }
    applyPose()
    apiRef.current.onPose({ x: st.x, w: petW })
    raf = requestAnimationFrame(tick)

    return () => {
      cancelAnimationFrame(raf)
      apiRef.current.onPose({ x: -99999, w: 0 })
    }
  }, [size, leafId, pet.slug, pet.spritesheetUrl, onPose])

  const handleClick = () => {
    if (dragMovedRef.current) {
      dragMovedRef.current = false
      return
    }
    const st = stateRef.current
    const now = performance.now()
    if (st) {
      st.walking = false
      st.flashState = 'waving'
      st.flashUntil = now + 700
      st.decisionAt = now + 800
    }
    bubbleDismissedRef.current = true
    setShowBubble(false)
    onClick()
  }

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    const el = elRef.current
    if (!el) return
    draggingRef.current = true
    dragMovedRef.current = false
    dragPointerIdRef.current = e.pointerId
    dragLastRef.current = { x: e.clientX, y: e.clientY }
    el.setPointerCapture(e.pointerId)
    el.style.cursor = 'grabbing'
  }

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current || dragPointerIdRef.current !== e.pointerId) return
    const st = stateRef.current
    if (!st) return
    const dx = e.clientX - dragLastRef.current.x
    const dy = e.clientY - dragLastRef.current.y
    dragLastRef.current = { x: e.clientX, y: e.clientY }
    if (Math.abs(dx) + Math.abs(dy) > 3) dragMovedRef.current = true
    const { containerW, containerH } = propsRef.current
    const { petW, petH } = size ?? { petW: 0, petH: 0 }
    if (petW <= 0 || petH <= 0) return
    st.x = Math.min(Math.max(0, containerW - petW), Math.max(0, st.x + dx))
    st.y = Math.min(Math.max(0, containerH - petH), Math.max(0, st.y + dy))
    st.walking = false
  }

  const handlePointerEnd = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragPointerIdRef.current !== e.pointerId) return
    const el = elRef.current
    if (el) {
      try {
        el.releasePointerCapture(e.pointerId)
      } catch {
        // already released
      }
      el.style.cursor = ''
    }
    draggingRef.current = false
    dragPointerIdRef.current = null
  }

  if (!size) return null
  return (
    <div
      ref={elRef}
      onClick={handleClick}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerEnd}
      onPointerCancel={handlePointerEnd}
      className="pointer-events-auto absolute left-0 top-0 cursor-grab select-none touch-none"
      title={pet.name}
      aria-hidden="true"
    >
      {showBubble && (
        <div className="pointer-events-none absolute bottom-full left-1/2 z-10 -translate-x-1/2 pb-1.5">
          <div className="whitespace-nowrap rounded-md border border-border bg-popover px-2 py-1 text-[11px] font-medium leading-none text-popover-foreground shadow-lg">
            Finished
          </div>
          <div className="mx-auto -mt-[5px] h-2 w-2 rotate-45 border-b border-r border-border bg-popover" />
        </div>
      )}
    </div>
  )
}
