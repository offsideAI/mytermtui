#!/usr/bin/env python3
"""Render true-color ANSI frames (from cmd/screenshot) to PNG screenshots.

Usage: python3 scripts/ansi2png.py <ansi-dir> <png-dir>

Only needs Pillow. Draws each frame in Menlo (with Apple Symbols as a
glyph fallback) on a dark background, wrapped in a macOS-style window.
"""

import os
import re
import sys
import unicodedata

from PIL import Image, ImageDraw, ImageFont

FONT_SIZE = 15
PAD = 18
TITLEBAR = 34
RADIUS = 12

BG = (30, 30, 46)        # default background
FG = (205, 214, 244)     # default foreground
CHROME = (24, 24, 37)    # window chrome
OUTER = (0, 0, 0, 0)     # transparent page around the window

# Catppuccin-ish ANSI palette: 0-7 normal, 8-15 bright.
ANSI16 = [
    (69, 71, 90), (243, 139, 168), (166, 227, 161), (249, 226, 175),
    (137, 180, 250), (245, 194, 231), (148, 226, 213), (186, 194, 222),
    (88, 91, 112), (243, 139, 168), (166, 227, 161), (249, 226, 175),
    (137, 180, 250), (245, 194, 231), (148, 226, 213), (205, 214, 244),
]


def xterm256(n):
    if n < 16:
        return ANSI16[n]
    if n < 232:
        n -= 16
        levels = [0, 95, 135, 175, 215, 255]
        return (levels[n // 36], levels[(n // 6) % 6], levels[n % 6])
    v = 8 + 10 * (n - 232)
    return (v, v, v)


class Fonts:
    def __init__(self):
        self.regular = ImageFont.truetype("/System/Library/Fonts/Menlo.ttc", FONT_SIZE, index=0)
        try:
            self.bold = ImageFont.truetype("/System/Library/Fonts/Menlo.ttc", FONT_SIZE, index=1)
        except OSError:
            self.bold = self.regular
        # Menlo covers every glyph mytermtui emits; only ☁ looks better
        # from Arial Unicode (Menlo's cloud is a tiny blob).
        self.symbol = None
        p = "/System/Library/Fonts/Supplemental/Arial Unicode.ttf"
        if os.path.exists(p):
            self.symbol = ImageFont.truetype(p, FONT_SIZE + 3)
        ascent, descent = self.regular.getmetrics()
        self.cell_w = round(self.regular.getlength("M"))
        self.cell_h = ascent + descent

    SYMBOL_OVERRIDES = frozenset("☁")

    def pick(self, ch, bold):
        if ch in self.SYMBOL_OVERRIDES and self.symbol is not None:
            return self.symbol
        return self.bold if bold else self.regular


def char_width(ch):
    if unicodedata.east_asian_width(ch) in ("W", "F"):
        return 2
    if unicodedata.combining(ch):
        return 0
    return 1


SGR_RE = re.compile(r"\x1b\[([0-9;]*)m")


class Pen:
    def __init__(self):
        self.reset()

    def reset(self):
        self.fg = None
        self.bg = None
        self.bold = False
        self.reverse = False
        self.underline = False

    def apply(self, params):
        p = [int(x) if x else 0 for x in params.split(";")] if params else [0]
        i = 0
        while i < len(p):
            c = p[i]
            if c == 0:
                self.reset()
            elif c == 1:
                self.bold = True
            elif c in (21, 22):
                self.bold = False
            elif c == 4:
                self.underline = True
            elif c == 24:
                self.underline = False
            elif c == 7:
                self.reverse = True
            elif c == 27:
                self.reverse = False
            elif 30 <= c <= 37:
                self.fg = ANSI16[c - 30]
            elif 90 <= c <= 97:
                self.fg = ANSI16[c - 90 + 8]
            elif 40 <= c <= 47:
                self.bg = ANSI16[c - 40]
            elif 100 <= c <= 107:
                self.bg = ANSI16[c - 100 + 8]
            elif c == 39:
                self.fg = None
            elif c == 49:
                self.bg = None
            elif c in (38, 48) and i + 1 < len(p):
                target = "fg" if c == 38 else "bg"
                if p[i + 1] == 5 and i + 2 < len(p):
                    setattr(self, target, xterm256(p[i + 2]))
                    i += 2
                elif p[i + 1] == 2 and i + 4 < len(p):
                    setattr(self, target, tuple(p[i + 2:i + 5]))
                    i += 4
            i += 1


def render(path, out_path, fonts):
    with open(path, encoding="utf-8") as f:
        text = f.read().rstrip("\n")
    lines = text.split("\n")

    # First pass: measure the grid.
    ncols = 0
    for line in lines:
        col = 0
        for part in SGR_RE.split(line)[::2]:  # even indexes are text
            for ch in part:
                col += char_width(ch)
        ncols = max(ncols, col)
    nrows = len(lines)

    cw, ch_px = fonts.cell_w, fonts.cell_h
    grid_w, grid_h = ncols * cw, nrows * ch_px
    img_w = grid_w + 2 * PAD
    img_h = grid_h + 2 * PAD + TITLEBAR

    img = Image.new("RGBA", (img_w + 40, img_h + 40), OUTER)
    draw = ImageDraw.Draw(img)

    # Window chrome with traffic lights.
    ox, oy = 20, 20
    draw.rounded_rectangle([ox, oy, ox + img_w, oy + img_h], RADIUS, fill=CHROME)
    draw.rounded_rectangle([ox, oy + TITLEBAR, ox + img_w, oy + img_h],
                           0, fill=BG)
    for i, color in enumerate([(255, 95, 87), (255, 189, 46), (39, 201, 63)]):
        cx = ox + 22 + i * 22
        draw.ellipse([cx - 7, oy + TITLEBAR // 2 - 7, cx + 7, oy + TITLEBAR // 2 + 7],
                     fill=color)
    title = os.path.splitext(os.path.basename(out_path))[0].split("-", 1)[-1]
    tf = fonts.regular
    draw.text((ox + img_w / 2 - tf.getlength("mytermtui — " + title) / 2,
               oy + TITLEBAR // 2 - fonts.cell_h // 2),
              "mytermtui — " + title, font=tf, fill=(147, 153, 178))

    x0, y0 = ox + PAD, oy + TITLEBAR + PAD

    pen = Pen()
    for row, line in enumerate(lines):
        pen.reset()  # each frame line starts clean (lipgloss resets per line)
        col = 0
        parts = SGR_RE.split(line)
        for idx, part in enumerate(parts):
            if idx % 2 == 1:  # SGR parameters
                pen.apply(part)
                continue
            for ch in part:
                w = char_width(ch)
                if w == 0:
                    continue
                fg = pen.fg or FG
                bg = pen.bg
                if pen.reverse:
                    fg, bg = (bg or BG), (pen.fg or FG)
                x = x0 + col * cw
                y = y0 + row * ch_px
                if bg is not None:
                    draw.rectangle([x, y, x + cw * w, y + ch_px], fill=bg)
                if ch != " ":
                    font = fonts.pick(ch, pen.bold)
                    dy = -2 if font is fonts.symbol else 0
                    draw.text((x, y + dy), ch, font=font, fill=fg)
                if pen.underline:
                    draw.line([x, y + ch_px - 2, x + cw * w, y + ch_px - 2], fill=fg)
                col += w

    img.save(out_path)
    print("wrote", out_path)


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    src, dst = sys.argv[1], sys.argv[2]
    os.makedirs(dst, exist_ok=True)
    fonts = Fonts()
    for name in sorted(os.listdir(src)):
        if name.endswith(".ansi"):
            render(os.path.join(src, name),
                   os.path.join(dst, name[:-5] + ".png"), fonts)


if __name__ == "__main__":
    main()
