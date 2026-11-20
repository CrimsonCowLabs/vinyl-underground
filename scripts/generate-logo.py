#!/usr/bin/env python3
"""Generate the Vinyl Underground logo (SVG) and every PNG/ICO icon the UI uses.

The logo is a stylised vinyl record: a black disc with grooves, two light
reflections, a crimson label with a cream ring and a spindle hole.

The geometry constants are shared by two renderers: svg() writes the vector
logo and draw_logo() paints the same shapes through pycairo for the PNGs.
Change both when changing the design, so the vector and raster icons match.

Requires: python3 with pycairo and Pillow (Pillow is only used for the .ico).
Run from the repository root:

    python3 scripts/generate-logo.py
"""

import io
import math
import os

import cairo
from PIL import Image

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PUBLIC = os.path.join(ROOT, "ui", "public")

VIEW = 512  # SVG viewBox size; the record is centred in it
C = VIEW / 2

DISC = "#121212"
GROOVE = "#2c2c2c"
LABEL = "#c8102e"
CREAM = "#f5e6c8"

DISC_R = 240
LABEL_R = 92
LABEL_RING_R = 72
HOLE_R = 12
GROOVES = [216, 192, 168, 144, 120]
# (start, end) angles in degrees, clockwise from 3 o'clock, as in SVG/cairo
SHINES = [(-150, -105), (30, 75)]
SHINE_R = 180


def hex_rgb(colour):
    colour = colour.lstrip("#")
    return tuple(int(colour[i : i + 2], 16) / 255 for i in (0, 2, 4))


def arc_point(r, deg):
    a = math.radians(deg)
    return C + r * math.cos(a), C + r * math.sin(a)


def ring_path(outer, inner):
    """SVG path for a filled annulus (even-odd), leaving the centre transparent."""

    def circle(r):
        return (
            f"M{C - r:g},{C:g}a{r:g},{r:g} 0 1,0 {2 * r:g},0"
            f"a{r:g},{r:g} 0 1,0 {-2 * r:g},0z"
        )

    return circle(outer) + circle(inner)


def svg(mono=None):
    """Full-colour logo, or a single-colour silhouette when mono is a colour."""
    out = [
        '<?xml version="1.0" encoding="UTF-8"?>',
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {VIEW} {VIEW}">',
        "<title>Vinyl Underground</title>",
    ]
    if mono:
        # Disc with the label ring and spindle hole cut out.
        out.append(
            f'<path fill="{mono}" fill-rule="evenodd" d="'
            + ring_path(DISC_R, LABEL_RING_R + 6)
            + ring_path(LABEL_RING_R - 2, HOLE_R)
            + '"/>'
        )
    else:
        out.append(
            f'<path fill="{DISC}" fill-rule="evenodd" d="{ring_path(DISC_R, LABEL_R)}"/>'
        )
        for r in GROOVES:
            out.append(
                f'<circle cx="{C:g}" cy="{C:g}" r="{r}" fill="none" '
                f'stroke="{GROOVE}" stroke-width="4"/>'
            )
        for start, end in SHINES:
            x1, y1 = arc_point(SHINE_R, start)
            x2, y2 = arc_point(SHINE_R, end)
            out.append(
                f'<path d="M{x1:.2f},{y1:.2f}A{SHINE_R},{SHINE_R} 0 0,1 {x2:.2f},{y2:.2f}" '
                'fill="none" stroke="#ffffff" stroke-opacity="0.28" '
                'stroke-width="14" stroke-linecap="round"/>'
            )
        out.append(
            f'<path fill="{LABEL}" fill-rule="evenodd" d="{ring_path(LABEL_R, HOLE_R)}"/>'
        )
        out.append(
            f'<circle cx="{C:g}" cy="{C:g}" r="{LABEL_RING_R}" fill="none" '
            f'stroke="{CREAM}" stroke-width="6"/>'
        )
    out.append("</svg>")
    return "\n".join(out) + "\n"


def draw_logo(ctx, x, y, size):
    """Draw the full-colour logo into a size x size box at (x, y)."""
    ctx.save()
    ctx.translate(x, y)
    ctx.scale(size / VIEW, size / VIEW)
    ctx.set_fill_rule(cairo.FILL_RULE_EVEN_ODD)

    def annulus(outer, inner, colour):
        ctx.new_path()
        ctx.arc(C, C, outer, 0, 2 * math.pi)
        ctx.new_sub_path()
        ctx.arc(C, C, inner, 0, 2 * math.pi)
        ctx.set_source_rgb(*hex_rgb(colour))
        ctx.fill()

    annulus(DISC_R, LABEL_R, DISC)
    ctx.set_source_rgb(*hex_rgb(GROOVE))
    ctx.set_line_width(4)
    for r in GROOVES:
        ctx.new_path()
        ctx.arc(C, C, r, 0, 2 * math.pi)
        ctx.stroke()
    ctx.set_source_rgba(1, 1, 1, 0.28)
    ctx.set_line_width(14)
    ctx.set_line_cap(cairo.LINE_CAP_ROUND)
    for start, end in SHINES:
        ctx.new_path()
        ctx.arc(C, C, SHINE_R, math.radians(start), math.radians(end))
        ctx.stroke()
    annulus(LABEL_R, HOLE_R, LABEL)
    ctx.set_source_rgb(*hex_rgb(CREAM))
    ctx.set_line_width(6)
    ctx.new_path()
    ctx.arc(C, C, LABEL_RING_R, 0, 2 * math.pi)
    ctx.stroke()
    ctx.restore()


def png(width, height, logo_size, background=None):
    surface = cairo.ImageSurface(cairo.FORMAT_ARGB32, width, height)
    ctx = cairo.Context(surface)
    if background:
        ctx.set_source_rgb(*hex_rgb(background))
        ctx.paint()
    draw_logo(ctx, (width - logo_size) / 2, (height - logo_size) / 2, logo_size)
    buf = io.BytesIO()
    surface.write_to_png(buf)
    return buf.getvalue()


def write(path, data):
    mode = "w" if isinstance(data, str) else "wb"
    with open(path, mode) as f:
        f.write(data)
    print("wrote", os.path.relpath(path, ROOT))


def main():
    write(os.path.join(PUBLIC, "logo.svg"), svg())
    write(os.path.join(PUBLIC, "safari-pinned-tab.svg"), svg(mono="#000000"))

    # Transparent icons: (file, width, height, logo size)
    transparent = [
        (os.path.join(PUBLIC, "android-chrome-192x192.png"), 192, 192, 192),
        (os.path.join(PUBLIC, "android-chrome-512x512.png"), 512, 512, 512),
        (os.path.join(PUBLIC, "favicon-16x16.png"), 16, 16, 16),
        (os.path.join(PUBLIC, "favicon-32x32.png"), 32, 32, 32),
        # Windows tiles keep upstream's canvas sizes and leave a margin.
        (os.path.join(PUBLIC, "mstile-70x70.png"), 128, 128, 96),
        (os.path.join(PUBLIC, "mstile-144x144.png"), 144, 144, 96),
        (os.path.join(PUBLIC, "mstile-150x150.png"), 270, 270, 160),
        (os.path.join(PUBLIC, "mstile-310x150.png"), 558, 270, 180),
        (os.path.join(PUBLIC, "mstile-310x310.png"), 558, 558, 320),
        (os.path.join(ROOT, "ui", "src", "icons", "android-icon-192x192.png"), 192, 192, 192),
        # Subsonic getAvatar placeholder
        (os.path.join(ROOT, "resources", "logo-192x192.png"), 192, 192, 192),
    ]
    for path, w, h, size in transparent:
        write(path, png(w, h, size))

    # Apple touch icons are opaque; iOS would fill transparency with black.
    for size in (60, 76, 120, 152, 180):
        data = png(size, size, round(size * 0.86), background=CREAM)
        write(os.path.join(PUBLIC, f"apple-touch-icon-{size}x{size}.png"), data)
    write(
        os.path.join(PUBLIC, "apple-touch-icon.png"),
        png(180, 180, round(180 * 0.86), background=CREAM),
    )

    ico = Image.open(io.BytesIO(png(48, 48, 48)))
    path = os.path.join(PUBLIC, "favicon.ico")
    ico.save(path, format="ICO", sizes=[(16, 16), (32, 32), (48, 48)])
    print("wrote", os.path.relpath(path, ROOT))


if __name__ == "__main__":
    main()
