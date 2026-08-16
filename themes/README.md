# Caw Themes

Ready-made Caw customization presets based on popular color themes for VS Code and other editors.

**Caw Dark** and **Caw Light** are bundled with the app, so they are always available. The additional dark presets below are loaded from GitHub when you open the Appearance settings.

## Available themes

| Theme | File | Origin | Notes |
| --- | --- | --- | --- |
| **One Dark Pro** | [`presets/one-dark-pro.json`](presets/one-dark-pro.json) | Atom's iconic One Dark, ported to VS Code | Soft slate-grey background, balanced blue/green/orange palette |
| **Dracula** | [`presets/dracula.json`](presets/dracula.json) | Dracula (Zeno Rocha) | Deep purple-tinted background with vivid candy colors |
| **Monokai Pro** | [`presets/monokai-pro.json`](presets/monokai-pro.json) | Monokai Pro (Monokai) | The modern successor of the original 2006 Monokai scheme |
| **Tokyo Night** | [`presets/tokyo-night.json`](presets/tokyo-night.json) | Tokyo Night (Enkia) | Calm, neon-inspired night palette |

> Install counts are approximate VS Code Marketplace totals as of early 2026.

## How to apply

1. Open **Settings** (gear icon in the sidebar), then **Appearance**.
2. Select a bundled or GitHub preset from the **Theme** dropdown.
3. Select **Custom** to edit and apply the persisted JSON directly. Reset returns to **Caw Dark**.

The selected theme is applied immediately and persisted for every device connected to Caw. GitHub preset loading is optional: Caw Dark and Caw Light continue to work offline.

## Tuning

- **Theme name**: `uiTheme` is the name displayed in the dropdown (for example, `"Dracula"`).
- **Terminal and page background**: upload media in **Terminal background**, optionally apply it to the full page, and tune its opacity or darkness. Opacity also works without media when the browser permits transparent page backgrounds.
- **Font size**: tweak `editor.fontSize` and `terminal.fontSize` in Custom JSON.
- **Syntax colors**: the `editor.tokenColors` map controls code highlighting across every supported language (including JSON keys, values, numbers, keywords, and punctuation) and the terminal's ANSI palette.
- **Terminal colors**: add hex `terminal.*` entries to `colors` (for example, `"terminal.background"`, `"terminal.foreground"`, or `"terminal.red"`) to override the terminal's derived colors.

## Adding a theme

Theme files are standard Caw `CustomizationState` JSON (`version: 1`). The structure is:

- `colors` — UI tokens as HSL triplets (e.g. `"220 13% 18%"`), Monaco editor colors as hex (e.g. `"editor.background": "#282C34"`), and optional `terminal.*` hex overrides.
- `editor.tokenColors` — hex syntax colors for comments, strings, keywords, etc.
- `terminal` — terminal theme, font size, and background. Set `background.applyToPage` to `true` to extend the background and transparency to the full page.

Drop a new file in `themes/presets/`. Its `uiTheme` must be the theme name shown in the picker.
