"""Собирает шрифт со знаком белорусского рубля (U+20C5, утверждён НБРБ 27.01.2026).

Знака пока нет в Unicode-шрифтах (ожидается в Unicode 19, 2027), поэтому берём букву «Б» из Onest —
основного шрифта сайта — и добавляем горизонтальную черту через ствол в нижней левой части.
Получаются крошечные woff2 по одному на насыщенность; CSS подключает их через unicode-range,
так что из этого шрифта браузер берёт только сам знак, а копирование текста даёт честный U+20C5.

Запуск: pip install fonttools brotli && python3 scripts/fonts/build_rub.py
"""
import os, sys
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer
from fontTools import subset
from fontTools.ttLib.tables._g_l_y_f import GlyphCoordinates

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SRC = os.path.join(ROOT, "web/app/node_modules/@fontsource-variable/onest/files/onest-cyrillic-wght-normal.woff2")
OUT = os.path.join(ROOT, "web/app/public/fonts")
SIGN = 0x20C5

for w in (400, 500, 600, 700):
    f = instancer.instantiateVariableFont(TTFont(SRC), {"wght": w})
    gname = f.getBestCmap()[0x411]
    glyf = f["glyf"]
    g = glyf[gname]
    coords, ends, flags = g.getCoordinates(glyf)
    stem_l, stem_r = round(coords[0][0]), round(coords[4][0])      # ствол «Б»: внешний и внутренний край
    arm = round(coords[1][1] - coords[3][1])                 # толщина верхней перекладины
    stem = stem_r - stem_l
    ext = round(stem * 1.05)                          # насколько черта выходит влево за ствол
    shift = round(ext * 0.75)
    yc = round(coords[1][1] * 0.22)                   # высота черты — нижняя часть буквы
    t = round(arm * 0.92)
    x0, x1 = stem_l - ext, stem_r
    y0, y1 = yc - t // 2, yc - t // 2 + t
    pts = [(round(x) + shift, round(y)) for x, y in coords]
    bar = [(x0 + shift, y0), (x0 + shift, y1), (x1 + shift, y1), (x1 + shift, y0)]  # по часовой, как внешний контур
    g.coordinates = GlyphCoordinates(pts + bar)
    g.endPtsOfContours = list(ends) + [len(pts) + 3]
    g.flags = bytearray(flags) + bytearray([1, 1, 1, 1])
    g.numberOfContours = len(g.endPtsOfContours)
    g.program = None
    adv, lsb = f["hmtx"][gname]
    f["hmtx"][gname] = (round(adv) + shift, min(round(lsb) + shift, x0 + shift))
    g.recalcBounds(glyf)

    opts = subset.Options()
    opts.layout_features = []
    opts.name_IDs = []
    opts.hinting = False
    opts.notdef_outline = False
    s = subset.Subsetter(opts)
    s.populate(unicodes=[0x411])
    s.subset(f)
    for t_ in f["cmap"].tables:
        t_.cmap = {SIGN: gname}
    for name in ("GSUB", "GPOS", "GDEF", "STAT"):
        if name in f:
            del f[name]
    f["name"].names = []
    f["name"].setName("AutoInvest Rub", 1, 3, 1, 0x409)
    f["name"].setName("Regular", 2, 3, 1, 0x409)
    f["name"].setName(f"AutoInvestRub-{w}", 6, 3, 1, 0x409)
    f.flavor = "woff2"
    path = os.path.join(OUT, f"byn-sign-{w}.woff2")
    f.save(path)
    print(path, os.path.getsize(path), "bytes")
