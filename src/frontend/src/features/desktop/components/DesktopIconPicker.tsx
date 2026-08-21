import { useRef, useState, type ReactNode } from 'react'
import { ImageUp, Monitor, X } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from '@/components/dropdown-menu'
import { DesktopAppIcon, resolveDesktopIconFill } from './DesktopAppIcon'
import { DESKTOP_BRAND_ICONS } from '../constants/desktopBrandIcons'

const LUCIDE_CHOICES: { ref: string; label: string }[] = [
  { ref: 'lucide:Globe', label: 'Globe' },
  { ref: 'lucide:Gamepad2', label: 'Game' },
  { ref: 'lucide:AppWindow', label: 'Window' },
  { ref: 'lucide:Box', label: 'Box' },
  { ref: 'lucide:Code2', label: 'Code' },
  { ref: 'lucide:Terminal', label: 'Terminal' },
  { ref: 'lucide:Database', label: 'Database' },
  { ref: 'lucide:Layers', label: 'Layers' },
  { ref: 'lucide:Music', label: 'Music' },
  { ref: 'lucide:Video', label: 'Video' },
  { ref: 'lucide:MessageSquare', label: 'Chat' },
]

// Max upload size after downscaling, as data-URL character length (~bytes).
const MAX_ICON_CHARS = 96_000

interface DesktopIconPickerProps {
  appId: string
  icon?: string
  iconColor?: string
  onChange: (icon: string | undefined, iconColor: string | undefined) => void
}

// DesktopIconPicker lets the user pick an icon for a desktop app: the
// generic Monitor, a vendored Simple Icons brand, a generic lucide glyph,
// or an uploaded image (downscaled to 64px and stored as a data URL in
// prefs). Vector icons can be tinted via the color control rendered by the
// settings row when a vector icon is active.
export function DesktopIconPicker({ appId, icon, iconColor, onChange }: DesktopIconPickerProps): ReactNode {
  const [open, setOpen] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const pick = (ref: string | undefined) => {
    onChange(ref, ref === icon ? iconColor : undefined)
    setOpen(false)
  }

  const upload = async (file: File) => {
    const dataUrl = await downscaleToDataUrl(file)
    if (dataUrl) {
      onChange(dataUrl, undefined)
      setOpen(false)
    }
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          title="Change icon"
          className="cursor-pointer flex items-center justify-center h-8 w-8 shrink-0 rounded-md border border-input bg-background hover:border-ring transition-colors"
        >
          <DesktopAppIcon appId={appId} icon={icon} iconColor={iconColor} size={18} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64 p-2">
        {/* Generic glyphs */}
        <div className="grid grid-cols-6 gap-1">
          <IconTile selected={!icon} title="Default" onClick={() => pick(undefined)}>
            <Monitor size={16} />
          </IconTile>
          {LUCIDE_CHOICES.map((c) => (
            <IconTile
              key={c.ref}
              selected={icon === c.ref}
              title={c.label}
              onClick={() => pick(c.ref)}
              fill={resolveDesktopIconFill(c.ref, iconColor)}
            >
              <DesktopAppIcon appId={`${appId}-${c.ref}`} icon={c.ref} size={16} />
            </IconTile>
          ))}
        </div>

        {/* Brand icons */}
        <div className="mt-2 mb-1 text-[10px] font-medium text-muted-foreground">Brands</div>
        <div className="grid grid-cols-6 gap-1 max-h-44 overflow-y-auto">
          {DESKTOP_BRAND_ICONS.map((b) => (
            <IconTile
              key={b.slug}
              selected={icon === `si:${b.slug}`}
              title={b.title}
              onClick={() => pick(`si:${b.slug}`)}
              fill={resolveDesktopIconFill(`si:${b.slug}`, iconColor)}
            >
              <svg role="img" viewBox="0 0 24 24" width={16} height={16}>
                <path d={b.path} fill={resolveDesktopIconFill(`si:${b.slug}`, iconColor)} />
              </svg>
            </IconTile>
          ))}
        </div>

        {/* Upload */}
        <button
          type="button"
          onClick={() => fileRef.current?.click()}
          className="cursor-pointer mt-2 w-full flex items-center gap-2 rounded-md border border-dashed border-border px-2 py-1.5 text-[11px] text-muted-foreground hover:text-foreground hover:border-ring transition-colors"
        >
          <ImageUp className="h-3.5 w-3.5" />
          Upload image (PNG/SVG, resized to 64px)
        </button>
        <input
          ref={fileRef}
          type="file"
          accept="image/png,image/jpeg,image/svg+xml,image/webp"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) void upload(f)
            e.target.value = ''
          }}
        />
        {icon?.startsWith('data:image/') && (
          <button
            type="button"
            onClick={() => pick(undefined)}
            className="cursor-pointer mt-1 w-full flex items-center gap-2 rounded-md px-2 py-1 text-[10px] text-muted-foreground hover:text-destructive transition-colors"
          >
            <X className="h-3 w-3" />
            Remove custom image
          </button>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function IconTile({ children, selected, title, onClick, fill }: {
  children: ReactNode
  selected?: boolean
  title: string
  onClick: () => void
  fill?: string
}): ReactNode {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      style={fill ? { color: fill } : undefined}
      className={`cursor-pointer flex items-center justify-center h-8 w-8 rounded-md border transition-colors ${
        selected
          ? 'border-ring bg-accent/50 text-foreground'
          : 'border-transparent text-muted-foreground hover:border-border hover:text-foreground hover:bg-accent/30'
      }`}
    >
      {children}
    </button>
  )
}

// downscaleToDataUrl rasterizes an uploaded image to at most 64x64 PNG
// (retrying at 48/32px if the result is too large) so icons stay tiny in
// prefs. SVGs are drawn as-is onto the canvas by the browser.
async function downscaleToDataUrl(file: File): Promise<string | undefined> {
  const url = URL.createObjectURL(file)
  try {
    const img = new Image()
    await new Promise<void>((resolve, reject) => {
      img.onload = () => resolve()
      img.onerror = () => reject(new Error('invalid image'))
      img.src = url
    })
    for (const size of [64, 48, 32]) {
      const scale = Math.min(1, size / Math.max(img.width, img.height))
      const w = Math.max(1, Math.round(img.width * scale))
      const h = Math.max(1, Math.round(img.height * scale))
      const canvas = document.createElement('canvas')
      canvas.width = w
      canvas.height = h
      const ctx = canvas.getContext('2d')
      if (!ctx) return undefined
      ctx.drawImage(img, 0, 0, w, h)
      const dataUrl = canvas.toDataURL('image/png')
      if (dataUrl.length <= MAX_ICON_CHARS) return dataUrl
    }
    console.warn('desktop icon upload too large even after downscaling')
    return undefined
  } catch (err) {
    console.error('icon upload failed:', err)
    return undefined
  } finally {
    URL.revokeObjectURL(url)
  }
}
