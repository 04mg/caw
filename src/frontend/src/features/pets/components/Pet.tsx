import { useEffect, useMemo, useRef, useState } from 'react'
import { type AgentStatus } from '@/features/agents/types'
import { getPetState } from '../petStates'
import { type PetEntry } from '../petsStore'

// Base sprite state driven by the agent's live status.
const STATUS_STATE: Record<string, string> = {
  thinking: 'review',
  executing: 'review',
  waiting_input: 'waiting',
  idle: 'idle',
  crashed: 'failed',
  tool_failed: 'failed',
  interrupted: 'jumping',
}

function baseStateFromStatus(status: AgentStatus | undefined): string {
  if (!status) return 'idle'
  return STATUS_STATE[status.status] ?? 'idle'
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

export interface PetRange {
  x: number
  w: number
}

interface PetProps {
  pet: PetEntry
  leafId: string
  status: AgentStatus | undefined
  containerW: number
  containerH: number
  getOtherRanges: () => PetRange[]
  onPose: (pose: PetRange) => void
  onClick: () => void
}

interface PetState {
  x: number
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
  statusSeq: number | undefined
  statusTool: string
}

export function Pet({ pet, leafId, status, containerW, containerH, getOtherRanges, onPose, onClick }: PetProps) {
  const elRef = useRef<HTMLDivElement | null>(null)
  const [size, setSize] = useState<{ petW: number; petH: number; rows: number } | null>(null)
  const stateRef = useRef<PetState | null>(null)
  const propsRef = useRef({ containerW, containerH })
  propsRef.current = { containerW, containerH }
  const apiRef = useRef({ getOtherRanges, onPose })
  apiRef.current = { getOtherRanges, onPose }

  const jitterY = useMemo(() => -((hashString(leafId + pet.slug) % 7) + 1), [leafId, pet.slug])

  const scale = useMemo(() => {
    if (containerH <= 0) return MIN_SCALE
    return Math.min(MAX_SCALE, Math.max(MIN_SCALE, containerH / 640))
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
      const petW = Math.round(192 * scale)
      const petH = Math.round(208 * scale)
      setSize({ petW, petH, rows })
    }
    img.src = pet.spritesheetUrl
    return () => {
      cancelled = true
      img.onload = null
      img.src = ''
    }
  }, [pet.spritesheetUrl, scale])

  // Handle status transitions: one-shot flashes for tool_failed / interrupted
  // / new tool call.
  useEffect(() => {
    const st = stateRef.current
    if (!st) return
    st.status = status
    if (!status) return
    const now = performance.now()
    const s = status.status
    if (s === 'tool_failed') {
      st.flashState = 'failed'
      st.flashUntil = now + 1220
    } else if (s === 'interrupted') {
      st.flashState = 'jumping'
      st.flashUntil = now + 840
    } else if (s === 'executing' || s === 'thinking') {
      const newTool =
        (typeof status.sequence === 'number' && status.sequence !== st.statusSeq) ||
        (status.tool !== undefined && status.tool !== st.statusTool)
      if (newTool) {
        st.flashState = 'waving'
        st.flashUntil = now + 700
      }
    }
    if (typeof status.sequence === 'number') st.statusSeq = status.sequence
    if (status.tool !== undefined) st.statusTool = status.tool
  }, [status])

  // Main animation loop: movement + sprite frame stepping, driven
  // imperatively against the DOM to avoid per-frame React re-renders.
  useEffect(() => {
    if (!size) return
    const el = elRef.current
    if (!el) return
    const { petW, petH, rows } = size
    let st = stateRef.current
    if (!st) {
      st = {
        x: PAD,
        targetX: PAD,
        dir: 1,
        walking: false,
        decisionAt: 0,
        lastTick: performance.now(),
        flashState: null,
        flashUntil: 0,
        spriteState: 'idle',
        spriteStarted: performance.now(),
        status: undefined,
        statusSeq: undefined,
        statusTool: '',
      }
      stateRef.current = st
    }

    const { containerW, containerH } = propsRef.current
    const max = Math.max(PAD, containerW - petW - PAD)
    const x0 = PAD + (hashString(leafId + pet.slug + 'pos') % Math.max(1, Math.round(max - PAD)))
    st.x = Math.min(max, Math.max(PAD, x0))
    st.targetX = st.x
    st.lastTick = performance.now()

    el.style.backgroundImage = `url(${pet.spritesheetUrl})`
    el.style.backgroundSize = `${8 * petW}px ${rows * petH}px`
    el.style.width = `${petW}px`
    el.style.height = `${petH}px`
    el.style.imageRendering = 'pixelated'

    const applyPose = () => {
      const ground = containerH - petH + jitterY
      el.style.transform = `translate3d(${st.x}px, ${ground}px, 0)`
    }

    const decide = (now: number) => {
      const { containerW } = propsRef.current
      const maxX = Math.max(PAD, containerW - petW - PAD)
      const roll = Math.random()
      if (roll < 0.45) {
        st.decisionAt = now + 1500 + Math.random() * 2500
        return
      }
      if (roll < 0.8) {
        const others = apiRef.current.getOtherRanges()
        let target: number | null = null
        for (let i = 0; i < 12; i++) {
          const t = PAD + Math.random() * Math.max(1, maxX - PAD)
          const blocked = others.some(
            (r) => t < r.x + r.w + WALK_GAP && t + petW + WALK_GAP > r.x,
          )
          if (!blocked) {
            target = t
            break
          }
        }
        if (target !== null) {
          st.targetX = Math.min(maxX, Math.max(PAD, target))
          st.dir = st.targetX >= st.x ? 1 : -1
          st.walking = true
          st.decisionAt = now + 15000
          return
        }
        st.decisionAt = now + 800 + Math.random() * 800
        return
      }
      if (roll < 0.9) {
        st.flashState = 'waving'
        st.flashUntil = now + 700
        st.decisionAt = now + 800
        return
      }
      st.flashState = 'jumping'
      st.flashUntil = now + 840
      st.decisionAt = now + 900
    }

    let raf = 0
    const tick = (now: number) => {
      const { containerW } = propsRef.current
      const maxX = Math.max(PAD, containerW - petW - PAD)
      const dt = Math.min((now - st.lastTick) / 1000, 0.1)
      st.lastTick = now

      if (st.walking) {
        const speed = 90 * dt
        if (Math.abs(st.targetX - st.x) <= speed) {
          st.x = st.targetX
          st.walking = false
          st.decisionAt = now + 1500 + Math.random() * 2500
        } else {
          st.x += st.dir * speed
          if (st.x <= PAD || st.x >= maxX) {
            st.x = Math.min(maxX, Math.max(PAD, st.x))
            st.walking = false
            st.decisionAt = now + 1200 + Math.random() * 1200
          }
        }
      } else if (now >= st.decisionAt) {
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
  }, [size, leafId, pet.slug, pet.spritesheetUrl, jitterY, onPose])

  const handleClick = () => {
    const st = stateRef.current
    if (st) {
      const now = performance.now()
      st.walking = false
      const flash = Math.random() < 0.5 ? 'waving' : 'jumping'
      st.flashState = flash
      st.flashUntil = now + (flash === 'waving' ? 700 : 840)
      st.decisionAt = now + (flash === 'waving' ? 800 : 900)
    }
    onClick()
  }

  if (!size) return null
  return (
    <div
      ref={elRef}
      onClick={handleClick}
      className="pointer-events-auto absolute left-0 top-0 cursor-pointer select-none"
      title={pet.name}
      aria-hidden="true"
    />
  )
}
