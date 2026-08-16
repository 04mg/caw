# Caw Themes

Ready-made Caw customization presets based on the most popular color themes for VS Code and other editors.

All five themes are **dark**, cover the full Caw UI (sidebar, panels, dialogs), Monaco syntax highlighting, and terminal colors, and ship with no background image or transparency — paste one in and you're done.

## Available themes

| Theme | File | Origin | Notes |
| --- | --- | --- | --- |
| **One Dark Pro** | [`one-dark-pro.json`](one-dark-pro.json) | Atom's iconic One Dark, ported to VS Code | ~12M installs; soft slate-grey background, balanced blue/green/orange palette |
| **Dracula** | [`dracula.json`](dracula.json) | Dracula (Zeno Rocha) | ~10M installs; deep purple-tinted background with vivid candy colors |
| **Monokai Pro** | [`monokai-pro.json`](monokai-pro.json) | Monokai Pro (Monokai) | ~4M installs; the modern successor of the original 2006 Monokai scheme |
| **GitHub Dark** | [`github-dark.json`](github-dark.json) | GitHub Themes (Primer) | ~18M installs; the most-installed theme in the VS Code marketplace |
| **Tokyo Night** | [`tokyo-night.json`](tokyo-night.json) | Tokyo Night (Enkia) | ~3M installs; calm, neon-inspired night palette |

> Install counts are approximate VS Code Marketplace totals as of early 2026.

## How to apply

1. Open **Settings** (gear icon in the sidebar).
2. Open the **Appearance** section.
3. Scroll to **Customization JSON**.
4. Replace the JSON in the textarea with the contents of the theme file, e.g.:
   ```bash
   cat themes/tokyo-night.json
   ```
5. Click **Apply JSON**.

The theme is applied immediately and persisted for every device connected to Caw.

## Tuning

- **UI theme**: all presets set `uiTheme` to `"dark"`. The Light / Dark / System buttons above the JSON editor override this on top of the preset.
- **Terminal background**: the presets disable the terminal background image (`assetId: ""`). To add one, upload it in **Terminal background** after applying a theme.
- **Font size**: tweak `editor.fontSize` and `terminal.fontSize` in the JSON, or use the font size controls in the settings.
- **Syntax colors**: the `editor.tokenColors` map controls code highlighting and the terminal's ANSI palette.
- **Terminal colors**: add hex `terminal.*` entries to `colors` (for example, `"terminal.background"`, `"terminal.foreground"`, or `"terminal.red"`) to override the terminal's derived colors.

## Adding a theme

Theme files are standard Caw `CustomizationState` JSON (`version: 1`). The structure is:

- `colors` — UI tokens as HSL triplets (e.g. `"220 13% 18%"`), Monaco editor colors as hex (e.g. `"editor.background": "#282C34"`), and optional `terminal.*` hex overrides.
- `editor.tokenColors` — hex syntax colors for comments, strings, keywords, etc.
- `terminal` — terminal theme, font size, and background.

Drop a new file in `themes/` and add a row to the table above.
