"""Frame a raw panel screenshot for the README: rounded corners, a hairline border and a soft
shadow on a transparent canvas, so it sits well on GitHub's light and dark themes.

    python3 assets/src/frame.py in.png out.png [--crop x,y,w,h]
"""
import sys
from PIL import Image, ImageDraw, ImageFilter

def frame(src, dst, crop=None, radius=28, pad=56, border=(255, 255, 255, 34)):
    shot = Image.open(src).convert("RGBA")
    if crop:
        x, y, w, h = crop
        shot = shot.crop((x, y, x + w, y + h))
    w, h = shot.size
    mask = Image.new("L", (w, h), 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, w - 1, h - 1), radius=radius, fill=255)
    shot.putalpha(mask)

    canvas = Image.new("RGBA", (w + pad * 2, h + pad * 2), (0, 0, 0, 0))
    shadow = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    ImageDraw.Draw(shadow).rounded_rectangle((pad, pad + 18, pad + w, pad + h + 18), radius=radius, fill=(0, 0, 0, 110))
    canvas = Image.alpha_composite(canvas, shadow.filter(ImageFilter.GaussianBlur(26)))
    canvas.alpha_composite(shot, (pad, pad))
    ImageDraw.Draw(canvas).rounded_rectangle((pad, pad, pad + w - 1, pad + h - 1), radius=radius, outline=border, width=2)
    canvas.save(dst, optimize=True)

if __name__ == "__main__":
    crop = None
    if "--crop" in sys.argv:
        crop = tuple(int(v) for v in sys.argv[sys.argv.index("--crop") + 1].split(","))
    frame(sys.argv[1], sys.argv[2], crop)
