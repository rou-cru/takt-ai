# TUI Logo Generation

The checked-in TUI logo and the installer's mark are generated data, not a runtime image conversion. Regenerate all artifacts with:

```bash
go generate ./takt/tui/styles
```

The canonical source is the versioned `docs/assets/brand/takt-ai.png`. To use another square PNG, run the generator directly:

```bash
go run ./development/generate-logo -input /path/to/logo.png \
  -output takt/tui/styles/logo_generated.go \
  -blocks-output takt/tui/styles/logo_blocks_generated.go \
  -installer install.sh
```

The generator is plain Go with no external tools. The canonical PNG never changes; from it the generator builds a flat terminal variant:

- **Geometric rings.** The outer band, the dark gap and the inner ring are drawn as exact circles from radii measured on the PNG (`interiorEdge`, `innerRingEdge`, `gapEdge`, `outerBandEdge`, as fractions of half the side). This way the artwork's shading never mottles them. Everything beyond the outer band is clear, so the mark blends with the terminal canvas, and the image is cropped to the band, so the mark is centered and symmetric.
- **Flat palette.** Inside the inner ring, each pixel takes the nearest of the interior inks: dark (also the eyes, nose and whiskers), the white body and the pale chest. The palette holds the artwork's own colors, the indigo rings included. Volume and gloss are dropped on purpose, because at terminal resolution only flat regions keep clean edges.
- **Majority downsampling.** Each output pixel takes the ink with the most votes in its box, so no in-between shade appears. Dark votes weigh more, so the thin features (eyes, nose, whiskers, chest outline) survive the small variants.

Every variant comes in three character sets:

- **Octants (Unicode 16, `U+1CD00` block plus the older block characters), 2×4 pixels per cell.** Each cell keeps its two most common inks as foreground and background. This is the sharpest mark, used on terminals known to draw octants themselves (kitty, Ghostty, foot).
- **Half-blocks (`▀ ▄ █`), 1×2 pixels per cell.** Every terminal font has them, so any other terminal shows the mark as intended. Multiplexers and unknown terminals get this set.
- **Braille, without color.** This is the mono-mode mark (`NO_COLOR`, dumb terminals); its shape survives the loss of color.

`TAKT_LOGO_GLYPHS=octants` or `TAKT_LOGO_GLYPHS=halfblocks` overrides the detection (`styles.DetectGlyphs`).

Each set holds several sizes (`logoRows`: 32, 26, 20, 16, 12 and 8 rows; every variant is twice as wide as it is tall, so the ring stays round). The home draws the largest size that fits (`styles.Logo`); a terminal too small for every size shows the menu alone.

The generator writes these checked-in artifacts:

- `takt/tui/styles/logo_generated.go`: `generatedLogoBraille`.
- `takt/tui/styles/logo_blocks_generated.go`: `generatedLogoOctants` and `generatedLogoHalfBlocks`.
- `install.sh`: a 14-row half-block `print_logo` function between the `BEGIN/END GENERATED LOGO` markers. The installer prints it only on an interactive true-color terminal; elsewhere the `Takt AI` signature stands alone.

The shared `logoSpan` type lives in `takt/tui/styles/logo.go`; generated files carry only data. All carry a `development/generate-logo; DO NOT EDIT` marker and must only change through the generator.

The OpenCode side carries no generated logo: the deployed `takt-dag` TUI plugin and the terminal client use no image mark.
