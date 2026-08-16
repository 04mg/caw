import type { ITheme as TerminalTheme } from '@xterm/xterm'

export type SidebarOrder = 'workspace-explorer' | 'explorer-workspace'

export interface TerminalBackground {
  assetId: string
  overlay: number
  blur: number
  applyToPage: boolean
}

export interface ColorSchemes {
  dark: Record<string, string>
  light: Record<string, string>
}

export interface CustomizationState {
  version: 1
  uiTheme: string
  colors: ColorSchemes
  editor: {
    theme: 'dark' | 'light'
    fontSize: number
    minimap: boolean
    tokenColors: Record<string, string>
  }
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

const MONACO_TOKEN_COLORS: Record<string, string> = {
  comment: '#6A9955',
  string: '#CE9178',
  keyword: '#C586C0',
  number: '#B5CEA8',
  regexp: '#D16969',
  type: '#4EC9B0',
  class: '#4EC9B0',
  function: '#DCDCAA',
  variable: '#9CDCFE',
  constant: '#4FC1FF',
  delimiter: '#D4D4D4',
  tag: '#569CD6',
  attribute: '#9CDCFE',
}

const TERMINAL_COLORS: TerminalTheme = {
  background: '#000000',
  foreground: '#F0F0F0',
  cursor: '#F0F0F0',
  selectionBackground: '#264F78',
  black: '#2E2E2E',
  red: '#EB4129',
  green: '#ABE047',
  yellow: '#F6C744',
  blue: '#47A0F0',
  magenta: '#7B5CB0',
  cyan: '#64DBED',
  white: '#E5E9F0',
  brightBlack: '#565656',
  brightRed: '#EC5357',
  brightGreen: '#C0E17D',
  brightYellow: '#F9DA6A',
  brightBlue: '#6284CF',
  brightMagenta: '#A37BB7',
  brightCyan: '#76D7E8',
  brightWhite: '#F6F9FA',
}

const LIGHT_MONACO_COLORS: Record<string, string> = {
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

const DEFAULT_COLOR_SCHEMES: ColorSchemes = {
  dark: { ...DARK_COLORS, ...MONACO_COLORS },
  light: { ...LIGHT_COLORS, ...LIGHT_MONACO_COLORS },
}

export const DEFAULT_CUSTOMIZATION: CustomizationState = {
  version: 1,
  uiTheme: 'Caw Dark',
  colors: DEFAULT_COLOR_SCHEMES,
  editor: { theme: 'dark', fontSize: 12, minimap: true, tokenColors: MONACO_TOKEN_COLORS },
  terminal: {
    theme: 'dark',
    fontSize: 13,
    background: { assetId: '', overlay: 0.35, blur: 0, applyToPage: false },
  },
  layout: { sidebarOrder: 'workspace-explorer' },
}

export function bundledTheme(name: 'Caw Dark' | 'Caw Light'): CustomizationState {
  if (name === 'Caw Light') {
    return {
      ...DEFAULT_CUSTOMIZATION,
      uiTheme: name,
      colors: {
        dark: { ...DEFAULT_CUSTOMIZATION.colors.dark },
        light: { ...DEFAULT_CUSTOMIZATION.colors.light },
      },
      editor: { ...DEFAULT_CUSTOMIZATION.editor, theme: 'light', tokenColors: { ...DEFAULT_CUSTOMIZATION.editor.tokenColors } },
      terminal: { ...DEFAULT_CUSTOMIZATION.terminal, theme: 'light', background: { ...DEFAULT_CUSTOMIZATION.terminal.background } },
      layout: { ...DEFAULT_CUSTOMIZATION.layout },
    }
  }

  return {
    ...DEFAULT_CUSTOMIZATION,
    colors: {
      dark: { ...DEFAULT_CUSTOMIZATION.colors.dark },
      light: { ...DEFAULT_CUSTOMIZATION.colors.light },
    },
    editor: { ...DEFAULT_CUSTOMIZATION.editor, tokenColors: { ...DEFAULT_CUSTOMIZATION.editor.tokenColors } },
    terminal: { ...DEFAULT_CUSTOMIZATION.terminal, background: { ...DEFAULT_CUSTOMIZATION.terminal.background } },
    layout: { ...DEFAULT_CUSTOMIZATION.layout },
  }
}

export function normalizeCustomization(value: Partial<CustomizationState> | undefined): CustomizationState {
  const v = value ?? {}
  const colors = normalizeColorSchemes(v.colors)
  const legacyBackground = v.terminal?.background as TerminalBackground & { opacity?: number } | undefined
  const { opacity: _legacyOpacity, ...background } = legacyBackground ?? {}
  const legacyThemeNames: Record<string, string> = {
    light: 'Caw Light',
    dark: 'Caw Dark',
    system: 'Caw Dark',
  }
  return {
    ...DEFAULT_CUSTOMIZATION,
    ...v,
    uiTheme: legacyThemeNames[v.uiTheme || ''] || v.uiTheme || DEFAULT_CUSTOMIZATION.uiTheme,
    colors,
    editor: {
      ...DEFAULT_CUSTOMIZATION.editor,
      ...v.editor,
      tokenColors: { ...DEFAULT_CUSTOMIZATION.editor.tokenColors, ...v.editor?.tokenColors },
    },
    terminal: {
      ...DEFAULT_CUSTOMIZATION.terminal,
      ...v.terminal,
      background: { ...DEFAULT_CUSTOMIZATION.terminal.background, ...background },
    },
    layout: { ...DEFAULT_CUSTOMIZATION.layout, ...v.layout },
  }
}

function normalizeColorSchemes(value: ColorSchemes | Record<string, string> | undefined): ColorSchemes {
  const raw = value as unknown as Record<string, unknown> | undefined
  const hasSchemes = raw && typeof raw.dark === 'object' && typeof raw.light === 'object'
  if (hasSchemes) {
    return {
      dark: { ...DEFAULT_COLOR_SCHEMES.dark, ...(raw.dark as Record<string, string>) },
      light: { ...DEFAULT_COLOR_SCHEMES.light, ...(raw.light as Record<string, string>) },
    }
  }
  return {
    dark: { ...DEFAULT_COLOR_SCHEMES.dark, ...(value as Record<string, string> | undefined) },
    light: { ...DEFAULT_COLOR_SCHEMES.light },
  }
}

export function applyCustomization(value: CustomizationState) {
  const root = document.documentElement
  const light = value.editor.theme === 'light'
  root.classList.toggle('light', light)
  const colors = light ? value.colors.light : value.colors.dark
  const defaults = light ? LIGHT_COLORS : DARK_COLORS
  for (const [name, color] of Object.entries(colors)) {
    if (name in defaults) root.style.setProperty(`--${name}`, color)
  }
  window.dispatchEvent(new CustomEvent('caw:customization-updated', { detail: value }))
}

export function monacoTheme(value: CustomizationState) {
  const dark = value.editor.theme === 'dark'
  const colors = dark ? value.colors.dark : value.colors.light
  const fallback = dark ? MONACO_COLORS : LIGHT_MONACO_COLORS
  const tokens = value.editor.tokenColors
  const rules = Object.entries(tokens).map(([token, foreground]) => ({
    token,
    foreground: foreground.replace(/^#/, ''),
  }))
  const jsonRules = [
    ['comment.block.json', 'comment'],
    ['comment.line.json', 'comment'],
    ['string.value.json', 'string'],
    ['string.key.json', 'attribute'],
    ['number.json', 'number'],
    ['keyword.json', 'keyword'],
    ['delimiter.bracket.json', 'delimiter'],
    ['delimiter.array.json', 'delimiter'],
    ['delimiter.colon.json', 'delimiter'],
    ['delimiter.comma.json', 'delimiter'],
  ].map(([token, color]) => ({
    token,
    foreground: (tokens[color] || MONACO_TOKEN_COLORS[color]).replace(/^#/, ''),
  }))
  return {
    base: dark ? 'vs-dark' : 'vs',
    // Built-in themes include language-specific token rules (for example,
    // `keyword.go`) that override the user palette's generic `keyword` rule.
    // Keep the base only for the editor's dark/light chrome; token colors must
    // come exclusively from the customization so every language is themed.
    inherit: false,
    rules: [...rules, ...jsonRules],
    colors: Object.fromEntries(Object.keys(fallback).map((key) => [key, colors[key] || fallback[key]])),
  }
}

export function terminalTheme(value: CustomizationState, transparentBackground = false): TerminalTheme {
  const dark = value.terminal.theme === 'dark'
  const colors = dark ? value.colors.dark : value.colors.light
  const tokens = value.editor.tokenColors
  const get = (name: string, fallback: string) => colors[`terminal.${name}`] || fallback
  const background = transparentBackground
    ? '#00000000'
    : get('background', colors['editor.background'] || TERMINAL_COLORS.background!)
  const foreground = get('foreground', colors['editor.foreground'] || TERMINAL_COLORS.foreground!)

  return {
    ...TERMINAL_COLORS,
    background,
    foreground,
    cursor: get('cursor', foreground),
    selectionBackground: get('selectionBackground', colors['editor.selectionBackground'] || TERMINAL_COLORS.selectionBackground!),
    black: get('black', colors['editor.lineHighlightBackground'] || TERMINAL_COLORS.black!),
    red: get('red', tokens.regexp || TERMINAL_COLORS.red!),
    green: get('green', tokens.function || TERMINAL_COLORS.green!),
    yellow: get('yellow', tokens.string || TERMINAL_COLORS.yellow!),
    blue: get('blue', tokens.tag || TERMINAL_COLORS.blue!),
    magenta: get('magenta', tokens.keyword || TERMINAL_COLORS.magenta!),
    cyan: get('cyan', tokens.type || TERMINAL_COLORS.cyan!),
    white: get('white', foreground),
    brightBlack: get('brightBlack', colors['editorLineNumber.foreground'] || TERMINAL_COLORS.brightBlack!),
    brightRed: get('brightRed', tokens.regexp || TERMINAL_COLORS.brightRed!),
    brightGreen: get('brightGreen', tokens.attribute || tokens.function || TERMINAL_COLORS.brightGreen!),
    brightYellow: get('brightYellow', tokens.string || TERMINAL_COLORS.brightYellow!),
    brightBlue: get('brightBlue', tokens.constant || tokens.tag || TERMINAL_COLORS.brightBlue!),
    brightMagenta: get('brightMagenta', tokens.keyword || TERMINAL_COLORS.brightMagenta!),
    brightCyan: get('brightCyan', tokens.class || tokens.type || TERMINAL_COLORS.brightCyan!),
    brightWhite: get('brightWhite', foreground),
  }
}
