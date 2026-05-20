from __future__ import annotations

import argparse
import json
import shutil
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

import cairosvg
import numpy as np
from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_SOURCE = ROOT.parent / "image-1778954678928.png"
OUT_ROOT = ROOT / "brand-assets"


@dataclass(frozen=True)
class AssetSpec:
    category: str
    name: str
    box: tuple[int, int, int, int]
    method: str
    pad: int = 24
    trim: bool = True


EXTRACT_SPECS: tuple[AssetSpec, ...] = (
    AssetSpec("logo", "proxyhub-mark-gradient", (60, 85, 400, 365), "white_to_alpha", 32),
    AssetSpec("logo", "proxyhub-wordmark", (390, 115, 990, 330), "white_to_alpha", 28),
    AssetSpec("logo", "proxyhub-horizontal-logo", (60, 85, 995, 365), "white_to_alpha", 32),
    AssetSpec("diagram", "workflow", (1010, 125, 1625, 370), "white_to_alpha", 24),
    AssetSpec("diagram", "route-list", (1040, 145, 1145, 295), "white_to_alpha", 18),
    AssetSpec("diagram", "arrow-left", (1140, 160, 1225, 240), "white_to_alpha", 16),
    AssetSpec("diagram", "proxyhub-mark", (1210, 135, 1380, 300), "white_to_alpha", 18),
    AssetSpec("diagram", "arrow-right", (1370, 160, 1460, 240), "white_to_alpha", 16),
    AssetSpec("diagram", "badge-socks5", (1440, 155, 1605, 225), "white_to_alpha", 14),
    AssetSpec("diagram", "badge-http", (1440, 215, 1605, 280), "white_to_alpha", 14),
    AssetSpec("diagram", "label-route-zh", (1015, 290, 1180, 360), "white_to_alpha", 14),
    AssetSpec("diagram", "label-proxyhub-en", (1190, 290, 1400, 360), "white_to_alpha", 14),
    AssetSpec("diagram", "label-proxy-pool-zh", (1410, 290, 1625, 360), "white_to_alpha", 14),
    AssetSpec("app-icons", "proxyhub-app-dark-raw", (500, 425, 800, 720), "white_to_alpha", 20),
    AssetSpec("app-icons", "proxyhub-app-gradient-raw", (875, 430, 1170, 715), "white_to_alpha", 20),
    AssetSpec("app-icons", "proxyhub-app-light-board", (125, 420, 445, 720), "crop", 0, False),
    AssetSpec("app-icons", "proxyhub-app-mono-board", (1240, 425, 1535, 720), "crop", 0, False),
    AssetSpec("app-icons", "proxyhub-mark-mono", (1290, 470, 1490, 670), "white_to_alpha", 18),
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Extract ProxyHub brand assets from the preview board.")
    parser.add_argument(
        "--source",
        type=Path,
        default=DEFAULT_SOURCE,
        help="Preview board image path. Defaults to ../image-1778954678928.png next to the repo.",
    )
    parser.add_argument(
        "--out",
        type=Path,
        default=OUT_ROOT,
        help="Output directory. Defaults to ./brand-assets inside the repo.",
    )
    return parser.parse_args()


def ensure_dir(path: Path) -> Path:
    path.mkdir(parents=True, exist_ok=True)
    return path


def white_to_alpha(image: Image.Image, alpha_floor: int = 6) -> Image.Image:
    rgb = np.asarray(image.convert("RGB"), dtype=np.float32)
    alpha = 255.0 - rgb.min(axis=2)
    alpha = np.where(alpha < alpha_floor, 0.0, alpha)
    alpha_safe = np.maximum(alpha, 1.0)
    restored = (rgb - (255.0 - alpha[..., None])) * 255.0 / alpha_safe[..., None]
    restored = np.where(alpha[..., None] > 0, restored, 0.0)
    rgba = np.dstack([np.clip(restored, 0, 255), np.clip(alpha, 0, 255)])
    return Image.fromarray(rgba.astype(np.uint8), "RGBA")


def trim_rgba(image: Image.Image, threshold: int = 4) -> Image.Image:
    rgba = image.convert("RGBA")
    alpha = rgba.getchannel("A")
    mask = alpha.point(lambda px: 255 if px > threshold else 0)
    bbox = mask.getbbox()
    return rgba.crop(bbox) if bbox else rgba


def pad_image(image: Image.Image, pad: int) -> Image.Image:
    if pad <= 0:
        return image
    canvas = Image.new("RGBA", (image.width + pad * 2, image.height + pad * 2), (0, 0, 0, 0))
    canvas.paste(image, (pad, pad), image)
    return canvas


def crop_asset(source: Image.Image, spec: AssetSpec) -> Image.Image:
    cropped = source.crop(spec.box)
    if spec.method == "white_to_alpha":
        cropped = white_to_alpha(cropped)
    else:
        cropped = cropped.convert("RGBA")
    if spec.trim:
        cropped = trim_rgba(cropped)
    return pad_image(cropped, spec.pad)


def write_png(image: Image.Image, path: Path) -> None:
    ensure_dir(path.parent)
    image.save(path)


def write_svg(path: Path, markup: str) -> None:
    ensure_dir(path.parent)
    path.write_text(markup.strip() + "\n", encoding="utf-8")


def gradient_defs() -> str:
    return """
  <defs>
    <linearGradient id="proxyhub-mark-gradient" x1="48" y1="72" x2="472" y2="386" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#42d9b4"/>
      <stop offset="0.52" stop-color="#36afd8"/>
      <stop offset="1" stop-color="#2f72ff"/>
    </linearGradient>
  </defs>"""


def mark_shapes(paint: str) -> str:
    return f"""
  <!-- Step 1: internal connector lines, drawn first so the hub ring caps their starts. -->
  <g fill="none" stroke="{paint}" stroke-width="30" stroke-linecap="butt" stroke-linejoin="round">
    <path d="M345 182 421 106"/>
    <path d="M345 248 421 324"/>
    <path d="M102 215H267" stroke-linecap="round"/>
  </g>

  <!-- Step 2: regular point-top hexagon. -->
  <path d="M315 45 462 130 462 300 315 385 168 300 168 130Z"
        fill="none" stroke="{paint}" stroke-width="30" stroke-linecap="round" stroke-linejoin="round"/>

  <!-- Step 2.5: visible node caps on the two right-side branch junctions. -->
  <circle cx="409" cy="118" r="25" fill="{paint}"/>
  <circle cx="409" cy="312" r="25" fill="{paint}"/>

  <!-- Step 3: left speed lines outside the hexagon. -->
  <g fill="none" stroke="{paint}" stroke-width="30" stroke-linecap="round" stroke-linejoin="round">
    <path d="M56 130H106"/>
    <path d="M32 215H68"/>
    <path d="M56 300H106"/>
  </g>

  <!-- Step 4: central hub ring, drawn last to mask line joins cleanly. -->
  <circle cx="315" cy="215" r="40" fill="none" stroke="{paint}" stroke-width="30"/>
"""


def mark_svg_markup(variant: str) -> str:
    paints = {
        "gradient": "url(#proxyhub-mark-gradient)",
        "white": "#ffffff",
        "mono": "#1f2b42",
    }
    defs = gradient_defs() if variant == "gradient" else ""
    return f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 530 430" width="530" height="430" role="img" aria-label="ProxyHub mark">
{defs}
{mark_shapes(paints[variant])}</svg>"""


def wordmark_svg_markup() -> str:
    return """<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 660 170" width="660" height="170" role="img" aria-label="ProxyHub wordmark">
  <text x="0" y="126" fill="#061834" font-family="Segoe UI Variable, Segoe UI, Arial, sans-serif" font-size="128" font-weight="800">ProxyHub</text>
</svg>"""


def horizontal_logo_svg_markup() -> str:
    return f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1040 260" width="1040" height="260" role="img" aria-label="ProxyHub horizontal logo">
{gradient_defs()}
  <g transform="translate(50 36) scale(0.43)">
{mark_shapes("url(#proxyhub-mark-gradient)")}
  </g>
  <text x="320" y="190" fill="#061834" font-family="Segoe UI Variable, Segoe UI, Arial, sans-serif" font-size="142" font-weight="800">ProxyHub</text>
</svg>"""


def app_card_svg_markup(variant: str) -> str:
    backgrounds = {
        "light": '<rect x="92" y="92" width="840" height="840" rx="220" fill="#ffffff" filter="url(#card-shadow)"/>',
        "dark": '<rect x="92" y="92" width="840" height="840" rx="220" fill="#0f1e3b"/>',
        "gradient": '<rect x="92" y="92" width="840" height="840" rx="220" fill="url(#app-gradient)"/>',
        "mono": '<rect x="92" y="92" width="840" height="840" rx="220" fill="#ffffff" filter="url(#card-shadow)"/>',
    }
    paints = {
        "light": "url(#proxyhub-mark-gradient)",
        "dark": "url(#proxyhub-mark-gradient)",
        "gradient": "#ffffff",
        "mono": "#1f2b42",
    }
    return f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024" role="img" aria-label="ProxyHub app icon {variant}">
  <defs>
    <linearGradient id="proxyhub-mark-gradient" x1="48" y1="72" x2="472" y2="386" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#42d9b4"/>
      <stop offset="0.52" stop-color="#36afd8"/>
      <stop offset="1" stop-color="#2f72ff"/>
    </linearGradient>
    <linearGradient id="app-gradient" x1="92" y1="92" x2="932" y2="932" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#42d9b4"/>
      <stop offset="1" stop-color="#2f72ff"/>
    </linearGradient>
    <filter id="card-shadow" x="-20%" y="-20%" width="140%" height="140%">
      <feDropShadow dx="0" dy="18" stdDeviation="30" flood-color="#23304b" flood-opacity="0.12"/>
    </filter>
  </defs>
  {backgrounds[variant]}
  <g transform="translate(75 135) scale(1.75)">
{mark_shapes(paints[variant])}
  </g>
</svg>"""


def route_list_svg_markup() -> str:
    return """<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 120" width="120" height="120" role="img" aria-label="route list">
  <g fill="none" stroke="#a9a9a9" stroke-width="10" stroke-linecap="round">
    <path d="M38 24h58"/>
    <path d="M38 60h58"/>
    <path d="M38 96h58"/>
  </g>
  <g fill="#a9a9a9">
    <circle cx="16" cy="24" r="7"/>
    <circle cx="16" cy="60" r="7"/>
    <circle cx="16" cy="96" r="7"/>
  </g>
</svg>"""


def arrow_svg_markup() -> str:
    return """<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 96 64" width="96" height="64" role="img" aria-label="arrow">
  <path d="M10 32h62M52 14l22 18-22 18" fill="none" stroke="#a9a9a9" stroke-width="8" stroke-linecap="round" stroke-linejoin="round"/>
</svg>"""


def badge_svg_markup(label: str) -> str:
    return f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 150 52" width="150" height="52" role="img" aria-label="{label}">
  <rect x="0" y="0" width="150" height="52" rx="10" fill="#e6e6e6"/>
  <text x="75" y="34" text-anchor="middle" fill="#616161" font-family="Segoe UI Variable, Segoe UI, Arial, sans-serif" font-size="24" font-weight="600">{label}</text>
</svg>"""


def workflow_svg_markup() -> str:
    return f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 650 240" width="650" height="240" role="img" aria-label="ProxyHub workflow">
{gradient_defs()}
  <g transform="translate(0 35) scale(1.08)">{route_list_svg_markup()}</g>
  <g transform="translate(130 65)">{arrow_svg_markup()}</g>
  <g transform="translate(220 42) scale(0.31)">{mark_shapes("url(#proxyhub-mark-gradient)")}</g>
  <g transform="translate(415 65)">{arrow_svg_markup()}</g>
  <g transform="translate(525 42) scale(0.82)">{badge_svg_markup("SOCKS5")}</g>
  <g transform="translate(525 112) scale(0.82)">{badge_svg_markup("HTTP")}</g>
  <text x="55" y="225" text-anchor="middle" fill="#777" font-family="Segoe UI Variable, Segoe UI, Arial, sans-serif" font-size="24">代理线路</text>
  <text x="322" y="225" text-anchor="middle" fill="#777" font-family="Segoe UI Variable, Segoe UI, Arial, sans-serif" font-size="24">ProxyHub</text>
  <text x="585" y="225" text-anchor="middle" fill="#777" font-family="Segoe UI Variable, Segoe UI, Arial, sans-serif" font-size="24">本地代理池</text>
</svg>"""


def render_svg(svg_path: Path, png_path: Path, size: int) -> None:
    ensure_dir(png_path.parent)
    cairosvg.svg2png(url=str(svg_path), write_to=str(png_path), output_width=size, output_height=size)


def write_ico(png_source: Path, ico_path: Path) -> None:
    ensure_dir(ico_path.parent)
    image = Image.open(png_source).convert("RGBA")
    image.save(ico_path, sizes=[(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (16, 16)])


def checkerboard(size: tuple[int, int], cell: int = 16) -> Image.Image:
    image = Image.new("RGBA", size, (245, 245, 245, 255))
    draw = ImageDraw.Draw(image)
    for y in range(0, size[1], cell):
        for x in range(0, size[0], cell):
            if (x // cell + y // cell) % 2:
                draw.rectangle([x, y, x + cell - 1, y + cell - 1], fill=(232, 232, 232, 255))
    return image


def build_preview_sheet(out_root: Path, preview_path: Path) -> None:
    preview_dir = ensure_dir(preview_path.parent)
    items = [
        "extracted/logo/proxyhub-wordmark.png",
        "extracted/logo/proxyhub-horizontal-logo.png",
        "vector/proxyhub-mark-gradient.svg",
        "vector/proxyhub-wordmark.svg",
        "vector/proxyhub-horizontal-logo.svg",
        "exports/png/light/256.png",
        "exports/png/dark/256.png",
        "exports/png/gradient/256.png",
        "exports/png/mono/256.png",
    ]
    rows: list[tuple[str, Image.Image]] = []
    for rel_path in items:
        path = out_root / rel_path
        if path.suffix == ".svg":
            rendered_path = preview_dir / f"{path.stem}.png"
            cairosvg.svg2png(url=str(path), write_to=str(rendered_path), output_width=520)
            path = rendered_path
        image = Image.open(path).convert("RGBA")
        image.thumbnail((560, 180), Image.LANCZOS)
        rows.append((rel_path, image.copy()))

    width = 620
    row_height = 230
    sheet = Image.new("RGBA", (width, row_height * len(rows)), (255, 255, 255, 255))
    draw = ImageDraw.Draw(sheet)
    for index, (label, image) in enumerate(rows):
        y = index * row_height
        draw.text((16, y + 12), label, fill=(45, 45, 45, 255))
        sheet.alpha_composite(checkerboard((560, 180)), (16, y + 42))
        sheet.alpha_composite(image, (16 + (560 - image.width) // 2, y + 42 + (180 - image.height) // 2))
    sheet.convert("RGB").save(preview_path)


def relative_to_root(paths: Iterable[Path], base: Path) -> list[str]:
    return [str(path.relative_to(base)).replace("\\", "/") for path in paths]


def main() -> None:
    args = parse_args()
    source_path = args.source.resolve()
    out_root = args.out.resolve()
    if not source_path.exists():
        raise FileNotFoundError(f"Source image not found: {source_path}")

    source_dir = ensure_dir(out_root / "source")
    extracted_dir = ensure_dir(out_root / "extracted")
    vector_dir = ensure_dir(out_root / "vector")
    vector_diagram_dir = ensure_dir(vector_dir / "diagram")
    png_dir = ensure_dir(out_root / "exports" / "png")
    ico_dir = ensure_dir(out_root / "exports" / "ico")
    preview_dir = ensure_dir(out_root / "preview")

    source_copy = source_dir / "proxyhub-brand-board.png"
    shutil.copy2(source_path, source_copy)

    source_image = Image.open(source_copy).convert("RGBA")
    generated: list[Path] = [source_copy]

    extracted_pngs: dict[str, Path] = {}
    for spec in EXTRACT_SPECS:
        asset = crop_asset(source_image, spec)
        out_path = extracted_dir / spec.category / f"{spec.name}.png"
        write_png(asset, out_path)
        extracted_pngs[spec.name] = out_path
        generated.append(out_path)

    mark_svg = vector_dir / "proxyhub-mark-gradient.svg"
    wordmark_svg = vector_dir / "proxyhub-wordmark.svg"
    horizontal_svg = vector_dir / "proxyhub-horizontal-logo.svg"
    mark_white_svg = vector_dir / "proxyhub-mark-white.svg"
    mark_mono_svg = vector_dir / "proxyhub-mark-mono.svg"
    write_svg(mark_svg, mark_svg_markup("gradient"))
    write_svg(wordmark_svg, wordmark_svg_markup())
    write_svg(horizontal_svg, horizontal_logo_svg_markup())
    write_svg(mark_white_svg, mark_svg_markup("white"))
    write_svg(mark_mono_svg, mark_svg_markup("mono"))
    generated.extend([mark_svg, wordmark_svg, horizontal_svg, mark_white_svg, mark_mono_svg])

    diagram_svgs = [
        vector_diagram_dir / "route-list.svg",
        vector_diagram_dir / "arrow-right.svg",
        vector_diagram_dir / "badge-socks5.svg",
        vector_diagram_dir / "badge-http.svg",
        vector_diagram_dir / "workflow.svg",
    ]
    write_svg(diagram_svgs[0], route_list_svg_markup())
    write_svg(diagram_svgs[1], arrow_svg_markup())
    write_svg(diagram_svgs[2], badge_svg_markup("SOCKS5"))
    write_svg(diagram_svgs[3], badge_svg_markup("HTTP"))
    write_svg(diagram_svgs[4], workflow_svg_markup())
    generated.extend(diagram_svgs)

    card_svgs: dict[str, Path] = {}
    for variant in ("light", "dark", "gradient", "mono"):
        out_svg = vector_dir / f"proxyhub-app-{variant}.svg"
        write_svg(out_svg, app_card_svg_markup(variant))
        card_svgs[variant] = out_svg
        generated.append(out_svg)

    png_exports: dict[str, list[Path]] = {}
    for variant, svg_path in card_svgs.items():
        variant_dir = ensure_dir(png_dir / variant)
        exports = []
        for size in (1024, 512, 256, 128):
            out_png = variant_dir / f"{size}.png"
            render_svg(svg_path, out_png, size)
            exports.append(out_png)
            generated.append(out_png)
        png_exports[variant] = exports
        ico_path = ico_dir / f"proxyhub-{variant}.ico"
        write_ico(variant_dir / "256.png", ico_path)
        generated.append(ico_path)

    favicon_path = ico_dir / "favicon.ico"
    shutil.copy2(ico_dir / "proxyhub-gradient.ico", favicon_path)
    generated.append(favicon_path)

    preview_path = preview_dir / "asset-preview.png"
    build_preview_sheet(out_root, preview_path)
    generated.append(preview_path)

    manifest = {
        "source": str(source_copy.relative_to(out_root)).replace("\\", "/"),
        "extracted_pngs": {name: str(path.relative_to(out_root)).replace("\\", "/") for name, path in extracted_pngs.items()},
        "vector_svgs": relative_to_root(
            [mark_svg, wordmark_svg, horizontal_svg, mark_white_svg, mark_mono_svg, *card_svgs.values(), *diagram_svgs],
            out_root,
        ),
        "png_exports": {variant: relative_to_root(paths, out_root) for variant, paths in png_exports.items()},
        "ico_exports": relative_to_root(sorted(ico_dir.glob("*.ico")), out_root),
        "preview": str(preview_path.relative_to(out_root)).replace("\\", "/"),
    }
    manifest_path = out_root / "manifest.json"
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    generated.append(manifest_path)

    print(f"Generated {len(generated)} assets in {out_root}")


if __name__ == "__main__":
    main()
