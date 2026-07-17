# Terminal themes

Place xterm.js theme JSON files in this directory (or the configured `ui.themes_dir`) to make them available in the WebSSH settings dropdown.

These files are **xterm.js `ITheme` color maps**. There is no built-in light/dark type field; any `light`/`dark` wording in filenames is only a naming convention.

## File naming

- Filename stem becomes the theme id shown in the UI.
- Example: `my-theme.json` appears as `my-theme`.
- Files from the configured directory override embedded defaults with the same id.

## Format

Minimal required fields:

```json
{
  "background": "#1e1e2e",
  "foreground": "#cdd6f4"
}
```

Recommended full palette (xterm.js `ITheme`):

```json
{
  "background": "#1e1e2e",
  "foreground": "#cdd6f4",
  "cursor": "#f5e0dc",
  "cursorAccent": "#1e1e2e",
  "selectionBackground": "#585b70",
  "black": "#45475a",
  "red": "#f38ba8",
  "green": "#a6e3a1",
  "yellow": "#f9e2af",
  "blue": "#89b4fa",
  "magenta": "#f5c2e7",
  "cyan": "#94e2d5",
  "white": "#bac2de",
  "brightBlack": "#585b70",
  "brightRed": "#f38ba8",
  "brightGreen": "#a6e3a1",
  "brightYellow": "#f9e2af",
  "brightBlue": "#89b4fa",
  "brightMagenta": "#f5c2e7",
  "brightCyan": "#94e2d5",
  "brightWhite": "#a6adc8"
}
```

An optional `"name"` field is ignored for identity and may be used as metadata only.

## How to add a theme

1. Copy one of the default JSON files in this folder as a template.
2. Edit the colors.
3. Save it as `your-theme-name.json` into the server `ui.themes_dir` (default `./themes`).
4. Reload the WebSSH page; the dropdown refreshes from disk automatically.

No rebuild is required for directory themes. Built-in defaults remain available even if the directory is empty or missing.
