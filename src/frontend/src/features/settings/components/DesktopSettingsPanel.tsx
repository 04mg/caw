import { useEffect, useState } from 'react'
import { ExternalLink, Monitor, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/button'
import { ColorPicker } from '@/components/color-picker'
import { DesktopIconPicker } from '@/features/desktop/components/DesktopIconPicker'
import { resolveDesktopIconFill } from '@/features/desktop/constants/desktopIconFill'
import { DESKTOP_BRAND_ICON_BY_SLUG } from '@/features/desktop/constants/desktopBrandIcons'
import {
  getDesktopApps,
  getDesktopStream,
  setDesktopApps,
  setDesktopStream,
  subscribePrefs,
  type DesktopAppPref,
  type DesktopStreamPrefs,
} from '@/features/prefs/stores/prefsStore'

type SaveStatus = 'idle' | 'success' | 'error'

interface DesktopSettingsPanelProps {
  onSaveStatusChange?: (status: SaveStatus) => void
}

interface XpraStatus {
  xpraInstalled: boolean
  xpraVersion?: string
}

export function DesktopSettingsPanel({ onSaveStatusChange }: DesktopSettingsPanelProps) {
  const [status, setStatus] = useState<XpraStatus | null>(null)
  const [apps, setApps] = useState<DesktopAppPref[]>(() => getDesktopApps())
  const [stream, setStream] = useState<DesktopStreamPrefs>(() => getDesktopStream())

  useEffect(() => {
    let cancelled = false
    fetch('/api/desktop/status')
      .then((res) => (res.ok ? res.json() : null))
      .then((json) => {
        if (!cancelled && json?.data) setStatus(json.data as XpraStatus)
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    return subscribePrefs(() => {
      setApps(getDesktopApps())
      setStream(getDesktopStream())
    })
  }, [])

  const save = async (next: DesktopAppPref[]) => {
    const ok = await setDesktopApps(next)
    onSaveStatusChange?.(ok ? 'success' : 'error')
  }

  const saveStream = async (next: DesktopStreamPrefs) => {
    setStream(next)
    const ok = await setDesktopStream(next)
    onSaveStatusChange?.(ok ? 'success' : 'error')
  }

  const updateApp = (idx: number, patch: Partial<DesktopAppPref>) => {
    void save(apps.map((a, i) => (i === idx ? { ...a, ...patch } : a)))
  }

  const removeApp = (idx: number) => {
    void save(apps.filter((_, i) => i !== idx))
  }

  const addApp = () => {
    const id = `app-${Date.now().toString(36)}`
    void save([...apps, { id, label: '', cmd: [] }])
  }

  const setCmd = (idx: number, raw: string) => {
    // Split on whitespace; keep empty array while the user types nothing so
    // the entry stays editable without becoming invalid.
    const parts = raw.trim().length === 0 ? [] : raw.trim().split(/\s+/)
    updateApp(idx, { cmd: parts })
  }

  const installed = status?.xpraInstalled === true

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-sm font-medium mb-1">Desktop</h3>
        <p className="text-xs text-muted-foreground">Run graphical applications side by side with your agents.</p>
      </div>

      {/* Xpra availability — only shown when xpra is missing, so the user
          knows why desktop apps are unavailable and how to install it. */}
      {status != null && !installed && (
        <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
          <div className="flex items-center gap-2">
            <Monitor className="h-4 w-4 text-muted-foreground" />
            <span className="text-xs font-medium">Xpra</span>
            <span className="ml-auto inline-flex items-center rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
              Not installed
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <p className="text-[10px] text-muted-foreground">
              Caw uses Xpra to stream desktop apps into a pane. Install it on this device to enable desktop apps:
            </p>
            <code className="rounded bg-muted/50 px-2 py-1 text-[10px] font-mono text-foreground/80 select-all">
              sudo apt-get install xpra xpra-x11
            </code>
            <div className="flex items-center gap-3 text-[10px]">
              <a href="https://xpra.org/" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors">
                xpra.org <ExternalLink className="h-3 w-3" />
              </a>
              <a href="https://github.com/Xpra-org/xpra#installation" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors">
                Installation instructions <ExternalLink className="h-3 w-3" />
              </a>
            </div>
          </div>
        </div>
      )}

      {/* Configured apps */}
      <div className={`flex flex-col gap-3 ${installed ? '' : 'pointer-events-none opacity-50'}`}>
        <div className="flex items-center justify-between">
          <label className="text-xs font-medium">Applications</label>
          <Button variant="outline" size="sm" className="gap-1.5 h-7 px-2 cursor-pointer" onClick={addApp}>
            <Plus className="h-3.5 w-3.5" />
            Add App
          </Button>
        </div>
        <p className="text-[10px] text-muted-foreground -mt-1.5">
          These apps appear in the New Tab menu and launch in their own desktop pane.
        </p>

        {apps.length === 0 ? (
          <p className="text-xs text-muted-foreground/70 py-4 text-center border border-dashed border-border rounded-lg">
            No desktop apps configured yet.
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {apps.map((app, idx) => {
              const isVector = !!app.icon && (app.icon.startsWith('si:') || app.icon.startsWith('lucide:'))
              const effectiveColor = resolveDesktopIconFill(app.icon, app.iconColor)
              return (
                <div key={app.id} className="flex flex-col gap-2 rounded-lg border border-border p-3">
                  <div className="flex items-center gap-2">
                    <DesktopIconPicker
                      appId={app.id}
                      icon={app.icon}
                      iconColor={app.iconColor}
                      onChange={(icon, iconColor) => updateApp(idx, { icon, iconColor })}
                    />
                    <input
                      type="text"
                      value={app.label}
                      onChange={(e) => updateApp(idx, { label: e.target.value })}
                      placeholder="Name (e.g. Firefox)"
                      className="flex-1 px-2.5 py-1.5 rounded-md border border-input bg-background text-xs text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors"
                    />
                    <button
                      onClick={() => removeApp(idx)}
                      title="Remove app"
                      className="cursor-pointer p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                  <input
                    type="text"
                    value={app.cmd.join(' ')}
                    onChange={(e) => setCmd(idx, e.target.value)}
                    placeholder="Command (e.g. firefox-esr --new-window)"
                    className="w-full px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors"
                  />
                  {isVector && (
                    <div className="flex items-center gap-2">
                      <span className="text-[10px] text-muted-foreground">Icon color</span>
                      <ColorPicker
                        value={app.iconColor}
                        fallbackColor={effectiveColor}
                        resetLabel={resetLabel(app.icon)}
                        onChange={(color) => updateApp(idx, { iconColor: color })}
                      />
                      <button
                        type="button"
                        onClick={() => updateApp(idx, { iconColor: undefined })}
                        className="cursor-pointer text-[10px] text-muted-foreground hover:text-foreground transition-colors underline-offset-2 hover:underline"
                        title="Use the icon's default color"
                      >
                        {resetLabel(app.icon)}{!app.iconColor && brandHexLabel(app.icon) ? ` (#${brandHexLabel(app.icon)})` : ''}
                      </button>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {!installed && status != null && (
        <p className="text-[10px] text-muted-foreground">
          Install Xpra to configure desktop apps.
        </p>
      )}

      {/* Streaming quality */}
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-0.5">
          <label className="text-xs font-medium">Streaming quality</label>
          <p className="text-[10px] text-muted-foreground">
            Applies to new desktop sessions. Higher quality/slower speed uses more bandwidth.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="w-16 text-[10px] text-muted-foreground">Encoding</span>
          <select
            value={stream.encoding}
            onChange={(e) => void saveStream({ ...stream, encoding: e.target.value })}
            className="flex-1 px-2 py-1.5 rounded-md border border-input bg-background text-xs text-foreground outline-none focus:border-ring transition-colors cursor-pointer"
          >
            <option value="auto">Auto</option>
            <option value="jpeg">JPEG (best for photos/video)</option>
            <option value="png">PNG (lossless, heavy)</option>
            <option value="webp">WebP</option>
          </select>
        </div>
        <div className="flex items-center gap-2">
          <span className="w-16 text-[10px] text-muted-foreground">Quality</span>
          <input
            type="range"
            min={1}
            max={100}
            value={stream.quality}
            onChange={(e) => void saveStream({ ...stream, quality: Number(e.target.value) })}
            className="flex-1 accent-primary cursor-pointer"
          />
          <span className="w-8 text-right text-[10px] text-muted-foreground tabular-nums">{stream.quality}</span>
        </div>
        <div className="flex items-center gap-2">
          <span className="w-16 text-[10px] text-muted-foreground">Speed</span>
          <input
            type="range"
            min={1}
            max={100}
            value={stream.speed}
            onChange={(e) => void saveStream({ ...stream, speed: Number(e.target.value) })}
            className="flex-1 accent-primary cursor-pointer"
          />
          <span className="w-8 text-right text-[10px] text-muted-foreground tabular-nums">{stream.speed}</span>
        </div>
      </div>
    </div>
  )
}

// brandHexLabel returns the official Simple Icons hex for a 'si:' ref, for
// display next to the "Brand default" color reset.
function brandHexLabel(icon: string | undefined): string | undefined {
  if (!icon?.startsWith('si:')) return undefined
  return DESKTOP_BRAND_ICON_BY_SLUG[icon.slice(3)]?.hex
}

// resetLabel names the color reset action for the icon kind: brand marks
// fall back to their official color, generic glyphs to the theme color.
function resetLabel(icon: string | undefined): string {
  return icon?.startsWith('si:') ? 'Brand default' : 'Default'
}
