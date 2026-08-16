
export type SidebarOrder = 'workspace-explorer' | 'explorer-workspace'

export interface TerminalBackground {
  assetId: string
  opacity: number
  overlay: number
  blur: number
}

export interface CustomizationState {
  version: 1
  uiTheme: 'light' | 'dark' | 'system'
  colors: Record<string, string>
  editor: { theme: 'dark' | 'light'; fontSize: number; minimap: boolean }
  terminal: { theme: 'dark' | 'light'; fontSize: number; background: TerminalBackground }
  layout: { sidebarOrder: SidebarOrder }
}

export const DEFAULT_CUSTOMIZATION: CustomizationState = {
  version: 1,
  uiTheme: 'system',
  colors: {},
  editor: { theme: 'dark', fontSize: 12, minimap: true },
  terminal: {
    theme: 'dark',
    fontSize: 13,
    background: { assetId: '', opacity: 1, overlay: 0.35, blur: 0 },
  },
  layout: { sidebarOrder: 'workspace-explorer' },
}

const DARK_COLORS = {
  background: '0 0% 3.9%',
  foreground: '0 0% 98%',
  card: '0 0% 3.9%',
  'card-foreground': '0 0% 98%',
  popover: '0 0% 3.9%',
  'popover-foreground': '0 0% 98%',
  primary: '0 0% 98%',
  'primary-foreground': '0 0% 9%',
  secondary: '0 0% 14.9%',
  'secondary-foreground': '0 0% 98%',
  muted: '0 0% 14.9%',
  'muted-foreground': '0 0% 63.9%',
  accent: '0 0% 14.9%',
  'accent-foreground': '0 0% 98%',
  border: '0 0% 14.9%',
  input: '0 0% 14.9%',
  ring: '0 0% 83.1%',
}

export function normalizeCustomization(value: Partial<CustomizationState> | undefined): CustomizationState {
  const v = value ?? {}
  return {
    ...DEFAULT_CUSTOMIZATION,
    ...v,
    colors: v.colors ?? {},
    editor: { ...DEFAULT_CUSTOMIZATION.editor, ...v.editor },
    terminal: {
      ...DEFAULT_CUSTOMIZATION.terminal,
      ...v.terminal,
      background: { ...DEFAULT_CUSTOMIZATION.terminal.background, ...v.terminal?.background },
    },
    layout: { ...DEFAULT_CUSTOMIZATION.layout, ...v.layout },
  }
}

export function applyCustomization(value: CustomizationState) {
  const root = document.documentElement
  const followsSystem = value.uiTheme === 'system'
  const light = value.uiTheme === 'light' || (followsSystem && window.matchMedia('(prefers-color-scheme: light)').matches)
  root.classList.toggle('light', light)
  const defaults = light ? {} : DARK_COLORS
  for (const [name, color] of Object.entries({ ...defaults, ...value.colors } as Record<string, string>)) {
    root.style.setProperty(`--${name}`, color)
  }
  root.style.setProperty('--terminal-opacity', String(value.terminal.background.opacity))
  window.dispatchEvent(new CustomEvent('caw:customization-updated', { detail: value }))
}

export function monacoTheme(value: CustomizationState) {
  const dark = value.editor.theme === 'dark'
  const background = value.colors.background ? `hsl(${value.colors.background})` : dark ? '#000000' : '#ffffff'
  return {
    base: dark ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': background,
      'editorGutter.background': background,
      'minimap.background': background,
      'editorWidget.background': background,
      'editorSuggestWidget.background': background,
    },
  }
}
