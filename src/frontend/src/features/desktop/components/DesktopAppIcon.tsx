import { type ReactNode } from 'react'
import { Monitor, Globe, Gamepad2, AppWindow, Box, Code2, Terminal, Database, Layers, Music, Video, MessageSquare, type LucideIcon } from 'lucide-react'
import { DESKTOP_BRAND_ICON_BY_SLUG } from '../constants/desktopBrandIcons'

// Generic lucide glyphs offered in the picker, keyed by their 'lucide:' ref.
const LUCIDE_ICONS: Record<string, LucideIcon> = {
  Globe,
  Gamepad2,
  AppWindow,
  Box,
  Code2,
  Terminal,
  Database,
  Layers,
  Music,
  Video,
  MessageSquare,
}

// Icon reference stored on a desktop app pref:
//   'si:<slug>'              — vendored Simple Icons brand path
//   'lucide:<Name>'          — generic lucide-react glyph
//   'data:image/...;base64,' — user-uploaded image
// Anything else (undefined included) falls back to the generic Monitor icon.
export function resolveDesktopIconFill(icon: string | undefined, iconColor: string | undefined): string {
  if (iconColor) return iconColor
  if (icon?.startsWith('si:')) {
    const brand = DESKTOP_BRAND_ICON_BY_SLUG[icon.slice(3)]
    // White brand marks are invisible on light backgrounds — fall back to
    // the surrounding text color so they adapt to the theme.
    if (brand && brand.hex.toUpperCase() !== 'FFFFFF') return `#${brand.hex}`
  }
  return 'currentColor'
}

interface DesktopAppIconProps {
  appId: string
  icon?: string
  iconColor?: string
  size?: number
  className?: string
}

// DesktopAppIcon renders the configured icon for a desktop app. Vector
// icons (si:/lucide:) are tinted with iconColor (or the brand's official
// color); uploads render as-is.
export function DesktopAppIcon({ appId, icon, iconColor, size = 16, className }: DesktopAppIconProps): ReactNode {
  const title = `desktop-app-icon-${appId}`
  if (icon?.startsWith('si:')) {
    const brand = DESKTOP_BRAND_ICON_BY_SLUG[icon.slice(3)]
    if (brand) {
      return (
        <svg
          role="img"
          viewBox="0 0 24 24"
          width={size}
          height={size}
          className={`shrink-0 ${className ?? ''}`}
          aria-label={brand.title}
          data-app-icon={title}
        >
          <path d={brand.path} fill={resolveDesktopIconFill(icon, iconColor)} />
        </svg>
      )
    }
  }
  if (icon?.startsWith('lucide:')) {
    const Icon = LUCIDE_ICONS[icon.slice(7)]
    if (Icon) {
      return <Icon size={size} className={`shrink-0 ${className ?? ''}`} data-app-icon={title} color={resolveDesktopIconFill(icon, iconColor)} />
    }
  }
  if (icon?.startsWith('data:image/')) {
    return (
      <img
        src={icon}
        alt=""
        width={size}
        height={size}
        className={`shrink-0 object-contain ${className ?? ''}`}
        data-app-icon={title}
      />
    )
  }
  return <Monitor size={size} className={`shrink-0 ${className ?? ''}`} data-app-icon={title} />
}
