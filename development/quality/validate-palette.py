"""Validates the palette tables of product/brand.md: decreasing luminance, text, action, state, limit and focus contrast on the intended backgrounds."""
import re
from pathlib import Path

doc = Path(__file__).resolve().parents[2].joinpath("product", "brand.md").read_text(encoding="utf-8")
palette = {"white": "#FFFFFF", "black": "#000000"}
tokens = {}
for line in doc.splitlines():
    cells = [c.strip().strip("`") for c in line.strip().strip("|").split("|")]
    if len(cells) == 6 and cells[0].isdigit():
        for family, value in zip("NPGAR", cells[1:]):
            assert re.fullmatch(r"#[0-9A-F]{6}", value), value
            palette[family + cells[0]] = value
    if len(cells) == 3 and re.fullmatch(r"[a-z]+(?:\.[a-z]+)+", cells[0]):
        tokens[cells[0]] = cells[1:]

def luminance(key):
    value = palette[key].lstrip("#")
    rgb = [int(value[i:i + 2], 16) / 255 for i in (0, 2, 4)]
    linear = [v / 12.92 if v <= 0.04045 else ((v + 0.055) / 1.055) ** 2.4
              for v in rgb]
    return sum(v * w for v, w in zip(linear, (0.2126, 0.7152, 0.0722)))

def contrast(a, b):
    low, high = sorted((luminance(a), luminance(b)))
    return (high + 0.05) / (low + 0.05)

checks = []
def check(fg, bg, minimum, label):
    actual = contrast(fg, bg)
    assert actual >= minimum, f"{label}: {fg}/{bg} = {actual:.4f}, requires {minimum}"
    checks.append(actual)

levels = (50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950)
for family in "NPGAR":
    ramp = [luminance(f"{family}{level}") for level in levels]
    assert all(a > b for a, b in zip(ramp, ramp[1:])), family

for theme in (0, 1):
    def value(name):
        return tokens[name][theme]

    backgrounds = ("bg.canvas", "bg.surface", "bg.raised", "bg.sunken", "bg.hover")
    for bg in backgrounds:
        for fg in ("text.primary", "text.secondary", "text.muted",
                   "brand.ink", "link.default", "link.hover"):
            check(value(fg), value(bg), 4.5, f"theme {theme}: {fg}/{bg}")
        for edge in ("border.control", "focus.ring"):
            check(value(edge), value(bg), 3, f"theme {theme}: {edge}/{bg}")
    for group in ("action.primary", "action.danger"):
        for state in ("bg", "hover", "pressed"):
            check(value(group + ".fg"), value(group + "." + state), 4.5, group)
    for group in ("info", "success", "warning", "danger", "selection", "disabled"):
        check(value(group + ".fg"), value(group + ".bg"), 4.5, group)
    check(value("selection.border"), value("selection.bg"), 3, "selection.border")
    check(value("focus.ring"), value("selection.bg"), 3, "focus on selection")

print(f"OK: 5 monotonic scales; {len(checks)} compliant pairs.")
