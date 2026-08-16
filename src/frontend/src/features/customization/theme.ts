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

const DARK_COLORS: Record<string, string> = {
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
  destructive: '0 62.8% 30.6%',
  'destructive-foreground': '0 0% 98%',
  border: '0 0% 14.9%',
  input: '0 0% 14.9%',
  ring: '0 0% 83.1%',
}

const LIGHT_COLORS: Record<string, string> = {
  background: '0 0% 100%',
  foreground: '0 0% 3.9%',
  card: '0 0% 100%',
  'card-foreground': '0 0% 3.9%',
  popover: '0 0% 100%',
  'popover-foreground': '0 0% 3.9%',
  primary: '0 0% 9%',
  'primary-foreground': '0 0% 98%',
  secondary: '0 0% 96.1%',
  'secondary-foreground': '0 0% 9%',
  muted: '0 0% 96.1%',
  'muted-foreground': '0 0% 45.1%',
  accent: '0 0% 96.1%',
  'accent-foreground': '0 0% 9%',
  destructive: '0 84.2% 60.2%',
  'destructive-foreground': '0 0% 98%',
  border: '0 0% 89.8%',
  input: '0 0% 89.8%',
  ring: '0 0% 3.9%',
}

const MONACO_COLORS: Record<string, string> = {
  'editor.background': '#000000',
  'editor.foreground': '#D4D4D4',
  'editorCursor.foreground': '#AEAFAD',
  'editor.selectionBackground': '#264F78',
  'editor.inactiveSelectionBackground': '#3A3D41',
  'editor.selectionHighlightBackground': '#ADD6FF26',
  'editor.lineHighlightBackground': '#2A2D2E',
  'editorLineNumber.foreground': '#858585',
  'editorLineNumber.activeForeground': '#C6C6C6',
  'editorIndentGuide.background': '#404040',
  'editorIndentGuide.activeBackground': '#707070',
  'editorWhitespace.foreground': '#404040',
  'editorBracketMatch.background': '#0064001A',
  'editorBracketMatch.border': '#888888',
  'editorWidget.background': '#252526',
  'editorWidget.foreground': '#CCCCCC',
  'editorWidget.border': '#454545',
  'editorSuggestWidget.background': '#252526',
  'editorSuggestWidget.foreground': '#D4D4D4',
  'editorSuggestWidget.border': '#454545',
  'editorSuggestWidget.selectedBackground': '#04395E',
  'editorHoverWidget.background': '#252526',
  'editorHoverWidget.foreground': '#CCCCCC',
  'editorHoverWidget.border': '#454545',
  'minimap.background': '#000000',
  'minimap.selectionHighlight': '#264F78',
  'scrollbarSlider.background': '#79797966',
  'scrollbarSlider.hoverBackground': '#646464B3',
  'scrollbarSlider.activeBackground': '#BFBFBF66',
}

export const DEFAULT_CUSTOMIZATION: CustomizationState = {
  version: 1,
  uiTheme: 'system',
  colors: { ...DARK_COLORS, ...MONACO_COLORS },
  editor: { theme: 'dark', fontSize: 12, minimap: true },
  terminal: {
    theme: 'dark',
    fontSize: 13,
    background: { assetId: '', opacity: 1, overlay: 0.35, blur: 0 },
  },
  layout: { sidebarOrder: 'workspace-explorer' },
}

export function normalizeCustomization(value: Partial<CustomizationState> | undefined): CustomizationState {
  const v = value ?? {}
  return {
    ...DEFAULT_CUSTOMIZATION,
    ...v,
    colors: { ...DEFAULT_CUSTOMIZATION.colors, ...v.colors },
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
  const defaults = light ? LIGHT_COLORS : DARK_COLORS
  for (const [name, color] of Object.entries({ ...defaults, ...value.colors })) {
    if (name in defaults) root.style.setProperty(`--${name}`, color)
  }
  root.style.setProperty('--terminal-opacity', String(value.terminal.background.opacity))
  window.dispatchEvent(new CustomEvent('caw:customization-updated', { detail: value }))
}

export function monacoTheme(value: CustomizationState) {
  const dark = value.editor.theme === 'dark'
  const colors = value.colors
  const fallback = dark ? MONACO_COLORS : {
    ...MONACO_COLORS,
    'editor.background': '#FFFFFF',
    'editor.foreground': '#000000',
    'editorLineNumber.foreground': '#237893',
    'editorLineNumber.activeForeground': '#0B216F',
    'editor.lineHighlightBackground': '#0000000F',
    'editor.selectionBackground': '#ADD6FF',
    'editorWidget.background': '#F3F3F3',
    'editorWidget.foreground': '#616161',
    'editorSuggestWidget.background': '#F3F3F3',
    'editorSuggestWidget.foreground': '#616161',
    'minimap.background': '#FFFFFF',
  }
  return {
    base: dark ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [],
    colors: Object.fromEntries(Object.keys(fallback).map((key) => [key, colors[key] || fallback[key]])),
  }
}
