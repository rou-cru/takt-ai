# TUI Logo Generation

The checked-in TUI logo, its image and the installer's mark are generated data, not a runtime image conversion. Regenerate all artifacts with:

```bash
go generate ./takt/tui/styles
```

The canonical source is the versioned `docs/assets/brand/takt-ai.png`. To use another square PNG, run the generator directly:

```bash
go run ./development/generate-logo -input /path/to/logo.png \
  -output takt/tui/styles/logo_generated.go \
  -blocks-output takt/tui/styles/logo_blocks_generated.go \
  -image-output takt/tui/styles/logo.png \
  -installer install.sh
```

The generator is plain Go with no external tools. The canonical PNG never changes; from it the generator builds a flat terminal variant:

- **Geometric rings.** The outer band, the dark gap and the inner ring are drawn as exact circles from radii measured on the PNG (`interiorEdge`, `innerRingEdge`, `gapEdge`, `outerBandEdge`, as fractions of half the side). This way the artwork's shading never mottles them. Everything beyond the outer band is clear, so the mark blends with the terminal canvas, and the image is cropped to the band, so the mark is centered and symmetric.
- **Flat palette.** Inside the inner ring, each pixel takes the nearest of the interior inks: dark (also the eyes, nose and whiskers), the white body and the pale chest. The palette holds the artwork's own colors, the indigo rings included. Volume and gloss are dropped on purpose, because at terminal resolution only flat regions keep clean edges.
- **Majority downsampling.** Each output pixel takes the ink with the most votes in its box, so no in-between shade appears. Dark votes weigh more, so the thin features (eyes, nose, whiskers, chest outline) survive the small variants.

The home draws the logo one of three ways:

- **Image (kitty graphics protocol).** The artwork itself, cropped to the outer band, with everything beyond it transparent so no square shows, at 512×512 pixels. The terminal scales it over the cells the home reserves for the logo. This is the sharpest mark.
- **Quadrants (`▖ ▗ ▘ ▝ ▚ ▞ ▙ ▛ ▜ ▟` and the half and full blocks), 2×2 pixels per cell.** Each cell keeps its two most common inks as foreground and background. Every terminal font has these characters. A cell is about twice as tall as wide, so its pixels are too, and the flat art is downsampled to twice as wide as tall to keep the ring round.
- **Braille, without color.** This is the mono-mode mark (`NO_COLOR`, dumb terminals); its shape survives the loss of color.

The mode is asked of the terminal, never chosen by its name. Before the TUI starts, `styles.ProbeLogoMode` sends a kitty graphics query that stores nothing, followed by a device-attributes request (DA1):

- an `OK` to the graphics query before the DA1 reply gets the image;
- a DA1 reply alone, no reply within 150 ms, or a non-terminal gets quadrants.

Every terminal answers DA1, so the wait ends as soon as the terminal has answered. `TAKT_LOGO=image` or `TAKT_LOGO=quadrants` overrides the measurement.

As an image, the logo occupies the cells of the quadrant variant that fits, filled with blank Braille cells. The home finds those cells on the rendered screen and places the stored image over them. It places the image again when the cells move and after a resize, hides it when a flow covers the home, and frees it from the terminal on exit.

The text marks hold three sizes (`logoRows`: 16, 12 and 8 rows; every variant is twice as wide as it is tall, so the ring stays round). The home caps the logo at 16 rows, about twice the menu's height, and draws the largest size that fits (`styles.Logo`); a terminal too small for every size shows the menu alone.

The generator writes these checked-in artifacts:

- `takt/tui/styles/logo_generated.go`: `generatedLogoBraille`.
- `takt/tui/styles/logo_blocks_generated.go`: `generatedLogoQuadrants`.
- `takt/tui/styles/logo.png`: the image, embedded in the binary.
- `install.sh`: a 14-row half-block `print_logo` function between the `BEGIN/END GENERATED LOGO` markers. The installer prints it only on an interactive true-color terminal; elsewhere the `Takt AI` signature stands alone.

The shared `logoSpan` type lives in `takt/tui/styles/logo.go`; generated files carry only data. The Go files carry a `development/generate-logo; DO NOT EDIT` marker, and every artifact must only change through the generator.

The OpenCode side carries no generated logo: the deployed `takt-dag` TUI plugin and the terminal client use no image mark.
