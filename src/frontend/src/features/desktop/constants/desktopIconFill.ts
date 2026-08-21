import { DESKTOP_BRAND_ICON_BY_SLUG } from './desktopBrandIcons'

// Resolves the fill color for a vector desktop app icon ('si:' or
// 'lucide:' refs): explicit tint first, then the brand's official color,
// falling back to currentColor so white brand marks adapt to the theme.
export function resolveDesktopIconFill(icon: string | undefined, iconColor: string | undefined): string {
  if (iconColor) return iconColor
  if (icon?.startsWith('si:')) {
    const brand = DESKTOP_BRAND_ICON_BY_SLUG[icon.slice(3)]
    if (brand && brand.hex.toUpperCase() !== 'FFFFFF') return `#${brand.hex}`
  }
  return 'currentColor'
}
