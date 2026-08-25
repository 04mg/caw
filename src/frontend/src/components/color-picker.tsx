import * as React from 'react'
import { Check } from 'lucide-react'
import { cn } from '@/features/shared/utils/utils'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from '@/components/dropdown-menu'

export interface ColorPickerProps {
  /** Currently selected color as '#RRGGBB', or undefined for the default. */
  value?: string
  /** Effective color shown on the trigger swatch when value is undefined. */
  fallbackColor?: string
  /** Label for the reset action, e.g. "Brand default" or "Default". */
  resetLabel?: string
  onChange: (color: string | undefined) => void
  className?: string
}

// Preset swatches — a compact Tailwind-style palette covering common hues
// plus neutrals. Ordered roughly by hue so the grid reads naturally.
const PRESET_COLORS = [
  '#000000', '#64748B', '#94A3B8', '#E2E8F0',
  '#EF4444', '#F97316', '#F59E0B', '#EAB308',
  '#84CC16', '#22C55E', '#10B981', '#14B8A6',
  '#06B6D4', '#0EA5E9', '#3B82F6', '#6366F1',
  '#8B5CF6', '#A855F7', '#EC4899', '#F43F5E',
]

/**
 * ColorPicker is a shadcn-style color picker: a small swatch button that
 * opens a dropdown with preset swatches, a custom color input, and a reset
 * entry. Replaces the raw native `<input type="color">` which rendered
 * inconsistently across platforms.
 */
const ColorPicker = React.forwardRef<HTMLButtonElement, ColorPickerProps>(
  ({ value, fallbackColor = 'currentColor', resetLabel = 'Default', onChange, className }, ref) => {
    const effective = value ?? fallbackColor
    return (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            ref={ref}
            type="button"
            title={value ? `Icon color ${value}` : 'Icon color'}
            className={cn(
              'cursor-pointer h-6 w-9 shrink-0 rounded-md border border-input bg-background p-0.5 transition-colors hover:border-ring focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
              className
            )}
          >
            <span className="block h-full w-full rounded-sm" style={{ backgroundColor: effective }} />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-56 p-2">
          <div className="grid grid-cols-5 gap-1">
            {PRESET_COLORS.map((c) => (
              <button
                key={c}
                type="button"
                title={c}
                onClick={() => onChange(c)}
                className="cursor-pointer group flex items-center justify-center h-7 w-7 rounded-md border border-transparent hover:border-border transition-colors"
              >
                <span
                  className="flex h-5 w-5 items-center justify-center rounded-sm border border-black/10 dark:border-white/20"
                  style={{ backgroundColor: c }}
                >
                  {value?.toUpperCase() === c && <Check className="h-3 w-3 text-white mix-blend-difference" />}
                </span>
              </button>
            ))}
          </div>
          <div className="mt-2 flex items-center gap-2">
            <label
              className="cursor-pointer relative h-6 w-9 shrink-0 rounded-md border border-input bg-background p-0.5 hover:border-ring transition-colors"
              title="Custom color"
            >
              <input
                type="color"
                value={/^#[0-9a-fA-F]{6}$/.test(value ?? '') ? value : '#000000'}
                onChange={(e) => onChange(e.target.value.toUpperCase())}
                className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
              />
              <span
                className="pointer-events-none block h-full w-full rounded-sm"
                style={{ background: 'linear-gradient(135deg, #ef4444, #f59e0b, #22c55e, #0ea5e9, #8b5cf6)' }}
              />
            </label>
            <span className="text-[11px] text-muted-foreground">Custom…</span>
            <button
              type="button"
              onClick={() => onChange(undefined)}
              disabled={!value}
              title={resetLabel}
              className="ml-auto cursor-pointer rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors disabled:opacity-40 disabled:pointer-events-none"
            >
              Reset
            </button>
          </div>
        </DropdownMenuContent>
      </DropdownMenu>
    )
  }
)
ColorPicker.displayName = 'ColorPicker'

export { ColorPicker }
