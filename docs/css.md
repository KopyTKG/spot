# Styling with CSS

spot is a GTK4 app, so styling is **GTK CSS**: a subset of web CSS with GTK's
own widget nodes. Layout comes from the TOML (widget order, `columns`, `width`...);
CSS only changes how things look: colours, borders, radii, fonts, padding.
There is no flexbox or grid.

Point a config at a stylesheet with `style` (path relative to the TOML, or
absolute):

```toml
style = "theme.css"
```

A complete theme to copy from: [`examples/theme.css`](../examples/theme.css).

## Contents

- [How the window is built](#how-the-window-is-built)
- [Class tree](#class-tree)
- [Class reference](#class-reference)
- [States and inner nodes](#states-and-inner-nodes)
- [Validation and errors](#validation-and-errors)
- [Opacity and transitions](#opacity-and-transitions)
- [Starter stylesheet](#starter-stylesheet)
- [Tips](#tips)

## How the window is built

The window is a layer-shell surface that **covers the whole output**, so that a
click outside the dialog can cancel it. The dialog is a box inside it, placed by
`anchor`:

- `.spot-window` is the full-screen surface. Keep it
  `background: transparent;` or the whole screen is tinted.
- `.spot-root` is the dialog itself: the card with the background, border,
  radius and padding.

```css
.spot-window { background: transparent; }

.spot-root {
  background: #1c1917;
  color: #fafaf9;
  border: 1px solid #44403c;
  border-radius: 12px;
  padding: 16px;
}
```

## Class tree

```
.spot-window
└── .spot-root
    ├── .spot-title
    ├── .spot-item                       one per [[items]]
    │   ├── .spot-label                  the item's label (not for toggle)
    │   ├── <the widget>                 see below
    │   └── .spot-item-error             if the item has an `error`
    ├── .spot-footer
    └── .spot-error                      shown when an exec fails
```

The widget inside an `.spot-item`, by `type`:

| `type` | Widget nodes |
|--------|--------------|
| `field` | `.spot-field` |
| `password` | `.spot-password` |
| `text` | `.spot-text-scroll` > `.spot-text` (+ `.spot-text-placeholder`) |
| `select` | `.spot-select` |
| `list` | `.spot-search` + `.spot-list-scroll` > `.spot-list` > rows (`.spot-list-row` labels) |
| `tiles` | `.spot-tiles` > `.spot-tile` > `.spot-tile-body` > `.spot-tile-icon` + `.spot-tile-label` |
| `toggle` | `.spot-toggle` |
| `scale` | `.spot-scale` |

## Class reference

| Class | Applies to |
|-------|-----------|
| `.spot-window` | Full-screen layer surface (keep transparent) |
| `.spot-root` | The dialog card |
| `.spot-title` | Heading from `title` |
| `.spot-item` | Container of one item (label + widget + error) |
| `.spot-label` | An item's `label` |
| `.spot-field` | `field` entry (`GtkEntry`) |
| `.spot-password` | `password` entry (`GtkPasswordEntry`) |
| `.spot-select` | `select` dropdown (`GtkDropDown`) |
| `.spot-search` | `list` search entry |
| `.spot-list-scroll` | `list` scroller |
| `.spot-list` | `list` view (`GtkListView`) |
| `.spot-list-row` | A row's label inside the list |
| `.spot-tiles` | `tiles` grid (`GtkFlowBox`) |
| `.spot-tile` | One tile (the flow-box child; carries `:selected`) |
| `.spot-picked` | Each tiles section's selected tile, kept even while the cursor is in another section, so every section shows its pending choice. Default: a light fill; the cursor outline is separate |
| `.spot-tile-pick` | A "●" marker inside every tile, visible only on the picked tile (so the pick shows even without CSS for `.spot-picked`) |
| `.spot-active` | The tile whose `value` equals the item's `current`; its body holds a `.spot-tile-badge` label |
| `.spot-cursor` | Added to the tile the keyboard cursor is on. The arrow keys move it across every tiles section without changing any section's pick; Enter picks the cursor tile and submits; a faint outline is the default, so override it in your CSS |
| `.spot-tile-body` | Box inside a tile holding icon and caption |
| `.spot-tile-icon` | A tile's `icon` |
| `.spot-tile-label` | A tile's `label` |
| `.spot-text-scroll` | `text` scroller |
| `.spot-text` | `text` view (`GtkTextView`) |
| `.spot-text-placeholder` | `text` placeholder label |
| `.spot-toggle` | `toggle` check button |
| `.spot-scale` | `scale` slider |
| `.spot-invalid` | Added to an item's input when `required` / `pattern` fails |
| `.spot-item-error` | An item's `error` message |
| `.spot-error` | Failed `exec` message (`on_error = "show"`) |
| `.spot-footer` | Footer text |

## States and inner nodes

GTK pseudo-classes work on these nodes:

| Selector | When |
|----------|------|
| `.spot-field:focus`, `.spot-password:focus`, `.spot-search:focus-within` | Entry has keyboard focus |
| `.spot-tile:hover`, `.spot-tile:selected` | Pointer over a tile / the selected tile |
| `.spot-tile:selected .spot-tile-label` | A tile's caption while selected |
| `.spot-list row:hover`, `.spot-list row:selected` | List rows (`row` is GTK's own node) |
| `.spot-list row:selected .spot-list-row` | The label of the selected row |

Widgets with inner nodes need the GTK element names:

```css
/* scale */
.spot-scale trough    { background: #0c0a09; border-radius: 6px; min-height: 6px; }
.spot-scale highlight { background: #7aa2f7; border-radius: 6px; }
.spot-scale slider    { background: #fafaf9; border-radius: 50%; min-width: 16px; min-height: 16px; }

/* text view: style the inner `text` node for padding and colour */
.spot-text, .spot-text text { background: transparent; color: #fafaf9; padding: 6px 10px; }

/* toggle: the box itself is `check` */
.spot-toggle check { border-radius: 4px; }
```

The GTK inspector shows the real node tree of a running dialog:
`GTK_DEBUG=interactive spot config.toml`.

## Validation and errors

When a submit fails `required` / `pattern`, spot adds `.spot-invalid` to the
item's input and shows its `error` as `.spot-item-error`. Both clear when the
item is edited. Where `.spot-invalid` lands, by type:

| `type` | Node that gets `.spot-invalid` |
|--------|--------------------------------|
| `field`, `password` | the entry |
| `select` | the dropdown |
| `list` | the search entry (`.spot-search`) |
| `text` | the `.spot-text` view |

```css
.spot-invalid    { border-color: #f7768e; }
.spot-item-error { font-size: 9pt; color: #f7768e; margin-left: 2px; }
.spot-error      { font-size: 9pt; color: #f7768e; margin-top: 6px; }  /* failed exec */
```

## Opacity and transitions

The fade from the [`[opacity]`](toml.md#opacity) table is applied by spot to
`.spot-root` at a higher priority than your stylesheet. **Don't set `opacity` or
`transition: opacity` on `.spot-root`**: it will be ignored. Use `[opacity]` in
the TOML instead. Transitions on other nodes (hover colours, for example) are
yours to set:

```css
.spot-tile { transition: background 120ms ease; }
```

For a translucent *background* rather than a faded dialog, use an alpha colour:

```css
.spot-root { background: rgba(28, 25, 23, 0.85); }
```

## Starter stylesheet

A minimal theme that covers every widget. Save it next to your config and set
`style` to it.

```css
* { font-family: "Inter", sans-serif; }

.spot-window { background: transparent; }

.spot-root {
  background: #1c1917;
  color: #fafaf9;
  border: 1px solid #44403c;
  border-radius: 12px;
  padding: 16px;
}

.spot-title  { font-size: 13pt; font-weight: bold; margin-bottom: 8px; }
.spot-label  { font-size: 10pt; color: #a8a29e; }
.spot-footer { font-size: 9pt;  color: #78716c; margin-top: 6px; }

/* inputs */
.spot-field, .spot-password, .spot-search {
  background: #0c0a09;
  border: 1px solid #44403c;
  border-radius: 8px;
  padding: 8px 12px;
  caret-color: #7aa2f7;
}
.spot-field:focus, .spot-password:focus, .spot-search:focus-within {
  border-color: #7aa2f7;
}

/* lists */
.spot-list-scroll, .spot-list { background: transparent; }
.spot-list row:selected { background: #44403c; }
.spot-list-row { padding: 8px 12px; }

/* tiles */
.spot-tile {
  background: #0c0a09;
  border: 1px solid #44403c;
  border-radius: 10px;
  margin: 4px;
  min-width: 96px;
  min-height: 96px;
}
.spot-tile:selected { background: rgba(122, 162, 247, 0.16); border-color: #7aa2f7; }
.spot-tile-icon     { font-size: 24pt; }

/* validation */
.spot-invalid, .spot-item-error, .spot-error { color: #f7768e; border-color: #f7768e; }
```

## Tips

- **Fonts and icons.** Set the font once with `* { font-family: ... }`. Tile
  icons are plain text, so use a Nerd Font family if you use glyphs.
- **Sizing.** `width` / `height` in the TOML size the dialog; CSS `min-width`
  and `min-height` size individual nodes (tiles, for instance).
- **Spacing between items** comes from GTK defaults; add `margin` on
  `.spot-item` to loosen or tighten it.
- **Debugging.** If a rule does nothing, check the node name in the GTK
  inspector. Inner nodes (`trough`, `slider`, `check`, `text`, `row`) are
  selected by element name, not by a `.spot-*` class.
- **One stylesheet, many configs.** Several configs can share one `style` file.
