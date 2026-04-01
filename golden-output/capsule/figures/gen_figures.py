#!/usr/bin/env python3
"""Generate SVG figures for the Capsule golden harvest report."""

import os

OUTPUT_DIR = os.path.dirname(os.path.abspath(__file__))


def generate_coverage_by_domain():
    """Bar chart: golden tests per domain."""
    domains = [
        ("tenant-isolation", 12, "#2563EB"),
        ("resource-quotas", 5, "#16A34A"),
        ("network-policies", 4, "#9333EA"),
    ]
    total = sum(d[1] for d in domains)

    bar_width = 80
    bar_gap = 40
    chart_height = 200
    max_val = max(d[1] for d in domains)
    scale = chart_height / max_val

    chart_width = len(domains) * (bar_width + bar_gap) + bar_gap
    svg_width = chart_width + 80  # left margin for y-axis
    svg_height = chart_height + 100  # bottom margin for labels + title

    left_margin = 60
    top_margin = 30
    bottom_margin = 70

    bars_svg = []
    for i, (label, count, color) in enumerate(domains):
        x = left_margin + bar_gap + i * (bar_width + bar_gap)
        bar_h = count * scale
        y = top_margin + (chart_height - bar_h)
        bars_svg.append(
            f'  <rect x="{x}" y="{y}" width="{bar_width}" height="{bar_h}" '
            f'fill="{color}" rx="4"/>'
        )
        # Count label above bar
        bars_svg.append(
            f'  <text x="{x + bar_width // 2}" y="{y - 6}" '
            f'text-anchor="middle" font-size="14" font-weight="bold" fill="#1F2937">{count}</text>'
        )
        # Domain label below bar (wrapped)
        words = label.split("-")
        for j, word in enumerate(words):
            bars_svg.append(
                f'  <text x="{x + bar_width // 2}" y="{top_margin + chart_height + 20 + j * 16}" '
                f'text-anchor="middle" font-size="12" fill="#4B5563">{word}</text>'
            )

    # Y-axis gridlines
    grid_lines = []
    for v in range(0, max_val + 1, 3):
        y_pos = top_margin + chart_height - v * scale
        grid_lines.append(
            f'  <line x1="{left_margin}" y1="{y_pos}" x2="{left_margin + chart_width}" '
            f'y2="{y_pos}" stroke="#E5E7EB" stroke-width="1"/>'
        )
        grid_lines.append(
            f'  <text x="{left_margin - 8}" y="{y_pos + 4}" text-anchor="end" '
            f'font-size="11" fill="#6B7280">{v}</text>'
        )

    total_label = f"Total: {total} golden tests"

    svg = f"""<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="{svg_width + 40}" height="{svg_height + 20}"
     viewBox="0 0 {svg_width + 40} {svg_height + 20}">
  <rect width="100%" height="100%" fill="white"/>

  <!-- Title -->
  <text x="{(svg_width + 40) // 2}" y="20" text-anchor="middle"
        font-size="15" font-weight="bold" fill="#111827">Golden Tests by Domain</text>
  <text x="{(svg_width + 40) // 2}" y="38" text-anchor="middle"
        font-size="11" fill="#6B7280">{total_label}</text>

  <!-- Grid -->
{''.join(grid_lines)}

  <!-- Bars -->
{''.join(bars_svg)}

  <!-- Y-axis -->
  <line x1="{left_margin}" y1="{top_margin}" x2="{left_margin}" y2="{top_margin + chart_height}"
        stroke="#9CA3AF" stroke-width="1.5"/>
  <!-- X-axis -->
  <line x1="{left_margin}" y1="{top_margin + chart_height}" x2="{left_margin + chart_width}"
        y2="{top_margin + chart_height}" stroke="#9CA3AF" stroke-width="1.5"/>

  <!-- Y-axis label -->
  <text transform="rotate(-90)" x="{-(top_margin + chart_height // 2)}" y="14"
        text-anchor="middle" font-size="12" fill="#6B7280">Tests</text>
</svg>"""

    out_path = os.path.join(OUTPUT_DIR, "coverage_by_domain.svg")
    with open(out_path, "w") as f:
        f.write(svg)
    print(f"Written: {out_path}")


def generate_test_progress():
    """Source-to-golden conversion funnel chart."""
    stages = [
        ("Source Tests\nextracted", 21, "#2563EB"),
        ("Harvested\n(accepted)", 21, "#16A34A"),
        ("Golden Tests\ncreated", 21, "#9333EA"),
        ("Deprecated /\nDeferred", 4, "#F59E0B"),
    ]

    svg_width = 520
    svg_height = 260
    bar_height = 36
    bar_gap = 18
    max_width = 340
    left_label_w = 130
    value_x_offset = 10
    max_val = 21

    items_svg = []
    for i, (label, count, color) in enumerate(stages):
        bar_w = int(max_width * count / max_val)
        y = 50 + i * (bar_height + bar_gap)
        items_svg.append(
            f'  <rect x="{left_label_w}" y="{y}" width="{bar_w}" height="{bar_height}" '
            f'fill="{color}" rx="4" opacity="0.85"/>'
        )
        items_svg.append(
            f'  <text x="{left_label_w + bar_w + value_x_offset}" y="{y + bar_height // 2 + 5}" '
            f'font-size="14" font-weight="bold" fill="#1F2937">{count}</text>'
        )
        # Multi-line label
        lines = label.split("\n")
        for j, line in enumerate(lines):
            items_svg.append(
                f'  <text x="{left_label_w - 8}" y="{y + bar_height // 2 - 4 + j * 16}" '
                f'text-anchor="end" font-size="12" fill="#374151">{line}</text>'
            )

    svg = f"""<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="{svg_width}" height="{svg_height}"
     viewBox="0 0 {svg_width} {svg_height}">
  <rect width="100%" height="100%" fill="white"/>

  <!-- Title -->
  <text x="{svg_width // 2}" y="22" text-anchor="middle"
        font-size="15" font-weight="bold" fill="#111827">Source to Golden Test Conversion</text>
  <text x="{svg_width // 2}" y="38" text-anchor="middle"
        font-size="11" fill="#6B7280">Capsule (21 source) → LoopZero golden suite (21 golden)</text>

{''.join(items_svg)}

  <!-- Legend note -->
  <text x="{left_label_w}" y="{svg_height - 12}" font-size="10" fill="#9CA3AF">
    * Deprecated/Deferred: cordoning (no LoopZero equiv.), ownerRef patch (design diff.), scalability (load test), PV isolation (deferred)
  </text>
</svg>"""

    out_path = os.path.join(OUTPUT_DIR, "test_progress.svg")
    with open(out_path, "w") as f:
        f.write(svg)
    print(f"Written: {out_path}")


if __name__ == "__main__":
    generate_coverage_by_domain()
    generate_test_progress()
    print("All figures generated.")
