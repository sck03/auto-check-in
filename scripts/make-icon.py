from pathlib import Path
from PIL import Image, ImageDraw

root = Path(__file__).resolve().parents[1]
image = Image.new("RGBA", (256, 256), (0, 0, 0, 0))
draw = ImageDraw.Draw(image)
draw.rounded_rectangle((8, 8, 248, 248), radius=66, fill="#167c69")
draw.line([(65, 129), (110, 172), (194, 84)], fill="white", width=18, joint="curve")
for x, y in [(65, 129), (194, 84)]:
    draw.ellipse((x - 9, y - 9, x + 9, y + 9), fill="white")
image.save(root / "build/windows/app.ico", sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
