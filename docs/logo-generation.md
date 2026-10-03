# TUI Logo Generation

The checked-in TUI logo and the installer's mark are generated data, not a runtime image conversion. Regenerate all artifacts with:

```bash
go generate ./takt/tui/styles
```

The canonical source is the versioned `docs/assets/brand/takt-ai.png`. To use another PNG, run the generator directly:

```bash
go run ./development/generate-logo -input /path/to/logo.png \
  -output takt/tui/styles/logo_generated.go \
  -blocks-output takt/tui/styles/logo_blocks_generated.go \
  -installer install.sh
```

Generation requires **Chafa 1.18.3**, installed on the maintainer machine (for example, `brew install chafa`). Chafa is deliberately not included in the released binary.

Before rendering, the generator prepares a terminal variant of the artwork; the canonical PNG never changes:

- **Inner-ring crop.** The PNG has a dark outer band, a black gap and the bright inner ring. Everything outside the gap (measured at 0.86 of the radius) is cleared: at terminal resolution the outer band only renders as a ragged fringe.
- **Border padding removal.** Near-white or near-black pixels connected to the edge become transparent, so the mark blends with the terminal canvas.
- **Petrol recolor.** Blue-to-indigo tones rotate onto the brand's petrol hue, keeping lightness and saturation, so the mark speaks the interface's single blue (brand.md §5). Neutrals — the white body, the dark interior — stay as they are.

From that image it produces Chafa datasets in two symbol sets and two sizes:

- **Braille, foreground-only** — the mono-mode mark. Its shape survives the loss of color when `NO_COLOR` or a dumb terminal strips styling.
- **Half-blocks with foreground and background truecolor** — the color-mode mark. Every cell carries two colors, so the logo tracks the artwork much closer than the Braille line art.
- **Full (48×20) and compact (24×10)** of each, so the home keeps a recognizable logo on short terminals (PR-UX-2A).

and writes them into checked-in artifacts:

- `takt/tui/styles/logo_generated.go` — `generatedLogo` and `generatedLogoCompact` (Braille), used by `styles.RenderLogo` and `styles.RenderCompactLogo` in mono mode.
- `takt/tui/styles/logo_blocks_generated.go` — `generatedLogoBlocks` and `generatedLogoBlocksCompact` (half-blocks), used in color mode.
- `install.sh` — a 32×14 half-block `print_logo` function between the `BEGIN/END GENERATED LOGO` markers. The installer prints it only on an interactive true-color terminal; elsewhere the `Takt AI` signature stands alone.

The shared `logoSpan` type lives in `takt/tui/styles/logo.go`; generated files carry only data. All carry a `development/generate-logo; DO NOT EDIT` marker and must only change through the generator.

The OpenCode side carries no generated logo: the deployed `takt-dag` TUI plugin and the terminal client use no image mark.
