#!/usr/bin/env python3
"""Generate SVG figures for the operators deep review report."""
import math
from pathlib import Path


def generate_findings_by_severity():
    """Bar chart: findings by severity bucket."""
    data = {"Critical": 0, "High": 7, "Medium": 21, "Low": 16}
    colors = {"Critical": "#b71c1c", "High": "#e53935", "Medium": "#ff9800", "Low": "#fdd835"}
    max_val = max(data.values())
    bar_w, bar_h_max = 90, 200
    gap, margin_l, margin_t, margin_b = 30, 60, 40, 60
    total_w = margin_l + (bar_w + gap) * len(data) + 20
    total_h = margin_t + bar_h_max + margin_b

    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="{total_w}" height="{total_h}">\n'
    svg += f'<rect width="{total_w}" height="{total_h}" fill="#fafafa"/>\n'
    svg += f'<text x="{total_w/2}" y="24" text-anchor="middle" font-family="sans-serif" font-size="15" font-weight="bold" fill="#212121">Findings by Severity (44 total)</text>\n'

    for i, (label, count) in enumerate(data.items()):
        bar_h = int(bar_h_max * count / max_val) if max_val > 0 else 0
        x = margin_l + i * (bar_w + gap)
        y = margin_t + bar_h_max - bar_h
        color = colors[label]
        svg += f'<rect x="{x}" y="{y}" width="{bar_w}" height="{bar_h}" fill="{color}" rx="4"/>\n'
        if count > 0:
            svg += f'<text x="{x + bar_w/2}" y="{y - 6}" text-anchor="middle" font-family="sans-serif" font-size="14" font-weight="bold" fill="#212121">{count}</text>\n'
        svg += f'<text x="{x + bar_w/2}" y="{margin_t + bar_h_max + 20}" text-anchor="middle" font-family="sans-serif" font-size="12" fill="#424242">{label}</text>\n'

    # Y axis line
    svg += f'<line x1="{margin_l - 5}" y1="{margin_t}" x2="{margin_l - 5}" y2="{margin_t + bar_h_max}" stroke="#bbb" stroke-width="1"/>\n'
    svg += '</svg>'
    return svg


def generate_findings_by_phase():
    """Bar chart: findings by review phase."""
    data = {"P2\nComplete.": 8, "P3\nArchit.": 4, "P4\nCode": 8, "P5\nInfra": 9, "P6\nSecurity": 6, "P7\nValidat.": 9}
    colors = ["#1565c0", "#0288d1", "#00897b", "#558b2f", "#f9a825", "#6a1b9a"]
    max_val = max(data.values())
    bar_w, bar_h_max = 70, 160
    gap, margin_l, margin_t, margin_b = 20, 50, 50, 60
    total_w = margin_l + (bar_w + gap) * len(data) + 20
    total_h = margin_t + bar_h_max + margin_b

    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="{total_w}" height="{total_h}">\n'
    svg += f'<rect width="{total_w}" height="{total_h}" fill="#fafafa"/>\n'
    svg += f'<text x="{total_w/2}" y="24" text-anchor="middle" font-family="sans-serif" font-size="15" font-weight="bold" fill="#212121">Findings by Review Phase</text>\n'

    for i, (label, count) in enumerate(data.items()):
        bar_h = int(bar_h_max * count / max_val) if max_val > 0 else 0
        x = margin_l + i * (bar_w + gap)
        y = margin_t + bar_h_max - bar_h
        color = colors[i % len(colors)]
        svg += f'<rect x="{x}" y="{y}" width="{bar_w}" height="{bar_h}" fill="{color}" rx="4"/>\n'
        svg += f'<text x="{x + bar_w/2}" y="{y - 6}" text-anchor="middle" font-family="sans-serif" font-size="13" font-weight="bold" fill="#212121">{count}</text>\n'
        for j, line in enumerate(label.split('\n')):
            svg += f'<text x="{x + bar_w/2}" y="{margin_t + bar_h_max + 18 + j*14}" text-anchor="middle" font-family="sans-serif" font-size="11" fill="#424242">{line}</text>\n'

    svg += f'<line x1="{margin_l - 5}" y1="{margin_t}" x2="{margin_l - 5}" y2="{margin_t + bar_h_max}" stroke="#bbb" stroke-width="1"/>\n'
    svg += '</svg>'
    return svg


def generate_findings_by_category():
    """Pie chart: findings by category."""
    data = {
        "Completeness": 9,
        "Code": 9,
        "Infrastructure": 9,
        "Security": 6,
        "Observability": 4,
        "Architecture": 4,
        "Testing": 1,
        "Operational": 2,
    }
    colors = ["#1565c0", "#e53935", "#558b2f", "#f57c00", "#6a1b9a", "#0288d1", "#00897b", "#78909c"]
    total = sum(data.values())

    cx, cy, r = 180, 170, 120
    legend_x, legend_y_start = 320, 50
    w, h = 520, 360

    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}">\n'
    svg += f'<rect width="{w}" height="{h}" fill="#fafafa"/>\n'
    svg += f'<text x="{w/2}" y="28" text-anchor="middle" font-family="sans-serif" font-size="15" font-weight="bold" fill="#212121">Findings by Category (44 total)</text>\n'

    start_angle = -math.pi / 2
    for i, (label, count) in enumerate(data.items()):
        angle = 2 * math.pi * count / total
        end_angle = start_angle + angle
        x1 = cx + r * math.cos(start_angle)
        y1 = cy + r * math.sin(start_angle)
        x2 = cx + r * math.cos(end_angle)
        y2 = cy + r * math.sin(end_angle)
        large_arc = 1 if angle > math.pi else 0
        color = colors[i % len(colors)]
        svg += f'<path d="M{cx},{cy} L{x1:.2f},{y1:.2f} A{r},{r} 0 {large_arc},1 {x2:.2f},{y2:.2f} Z" fill="{color}" stroke="white" stroke-width="2"/>\n'

        # Label in slice if large enough
        mid_angle = start_angle + angle / 2
        if count >= 4:
            lx = cx + (r * 0.65) * math.cos(mid_angle)
            ly = cy + (r * 0.65) * math.sin(mid_angle)
            pct = int(100 * count / total)
            svg += f'<text x="{lx:.1f}" y="{ly:.1f}" text-anchor="middle" font-family="sans-serif" font-size="11" font-weight="bold" fill="white">{pct}%</text>\n'

        start_angle = end_angle

        # Legend
        ly = legend_y_start + i * 22
        svg += f'<rect x="{legend_x}" y="{ly}" width="14" height="14" fill="{color}" rx="2"/>\n'
        svg += f'<text x="{legend_x + 20}" y="{ly + 11}" font-family="sans-serif" font-size="11" fill="#212121">{label} ({count})</text>\n'

    svg += '</svg>'
    return svg


def generate_risk_matrix():
    """3x3 risk heatmap with finding IDs placed by severity/probability."""
    # Placement:
    # HIGH severity: INFRA-001 (high prob), VAL-001 (high prob), CODE-001 (medium prob)
    #                INFRA-000 (medium prob), COMP-001+008 (medium prob), VAL-006 (medium prob)
    # MEDIUM severity: spread across medium/low probability
    # LOW severity: low probability columns

    cell_w, cell_h = 150, 100
    margin_l, margin_t = 110, 80
    probabilities = ["High", "Medium", "Low"]
    severities = ["Low", "Medium", "High"]  # left to right

    colors = [
        ["#ffeb3b", "#ff9800", "#e53935"],   # prob=High
        ["#c8e6c9", "#ffd54f", "#ff9800"],   # prob=Medium
        ["#e8f5e9", "#c8e6c9", "#ffd54f"],   # prob=Low
    ]

    # Findings per cell [prob_row][sev_col]
    findings_in_cell = [
        [[], [], ["INFRA-001", "VAL-001"]],           # prob=High
        [[], ["ARCH-001","CODE-002","CODE-006","SEC-001","INFRA-002","INFRA-003","INFRA-005"],
              ["CODE-001", "INFRA-000", "COMP-001", "COMP-008", "VAL-006"]],  # prob=Med
        [["ARCH-002","ARCH-003","ARCH-004","CODE-005","CODE-007","COMP-004","INFRA-006","INFRA-007","INFRA-008"],
         ["COMP-003","COMP-005","COMP-006","COMP-007","INFRA-004","OBS-001","VAL-002","SEC-002","SEC-003","VAL-003","CODE-003","CODE-004","VAL-005"],
         ["OBS-002"]]  # prob=Low
    ]

    total_w = margin_l + cell_w * 3 + 20
    total_h = margin_t + cell_h * 3 + 60

    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="{total_w}" height="{total_h}">\n'
    svg += f'<rect width="{total_w}" height="{total_h}" fill="#fafafa"/>\n'
    svg += f'<text x="{total_w/2}" y="28" text-anchor="middle" font-family="sans-serif" font-size="15" font-weight="bold" fill="#212121">Risk Matrix: Severity x Probability</text>\n'

    # Column headers (severity)
    for si, sev in enumerate(severities):
        x = margin_l + si * cell_w + cell_w / 2
        svg += f'<text x="{x}" y="{margin_t - 10}" text-anchor="middle" font-family="sans-serif" font-size="12" font-weight="bold" fill="#424242">{sev}</text>\n'
    svg += f'<text x="{margin_l + cell_w * 1.5}" y="{margin_t - 30}" text-anchor="middle" font-family="sans-serif" font-size="12" fill="#757575">Severity →</text>\n'

    # Row headers (probability)
    for pi, prob in enumerate(probabilities):
        y = margin_t + pi * cell_h + cell_h / 2 + 4
        svg += f'<text x="{margin_l - 10}" y="{y}" text-anchor="end" font-family="sans-serif" font-size="12" font-weight="bold" fill="#424242">{prob}</text>\n'
    svg += f'<text x="20" y="{margin_t + cell_h * 1.5}" text-anchor="middle" font-family="sans-serif" font-size="12" fill="#757575" transform="rotate(-90,20,{margin_t + cell_h * 1.5})">Probability →</text>\n'

    # Cells
    for pi, prob in enumerate(probabilities):
        for si, sev in enumerate(severities):
            x = margin_l + si * cell_w
            y = margin_t + pi * cell_h
            color = colors[pi][si]
            ids = findings_in_cell[pi][si]
            count = len(ids)
            svg += f'<rect x="{x}" y="{y}" width="{cell_w}" height="{cell_h}" fill="{color}" stroke="white" stroke-width="2"/>\n'
            if count > 0:
                svg += f'<text x="{x + cell_w/2}" y="{y + 22}" text-anchor="middle" font-family="sans-serif" font-size="14" font-weight="bold" fill="#212121">{count} findings</text>\n'
                # List up to 3 finding IDs
                for idx, fid in enumerate(ids[:3]):
                    svg += f'<text x="{x + cell_w/2}" y="{y + 40 + idx*16}" text-anchor="middle" font-family="monospace" font-size="10" fill="#424242">{fid}</text>\n'
                if count > 3:
                    svg += f'<text x="{x + cell_w/2}" y="{y + 40 + 3*16}" text-anchor="middle" font-family="sans-serif" font-size="10" fill="#424242">+{count - 3} more</text>\n'
            else:
                svg += f'<text x="{x + cell_w/2}" y="{y + cell_h/2 + 5}" text-anchor="middle" font-family="sans-serif" font-size="13" fill="#9e9e9e">—</text>\n'

    svg += '</svg>'
    return svg


def generate_dependency_graph():
    """DAG showing controllers -> provisioners -> external systems."""
    w, h = 720, 480

    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}">\n'
    svg += f'<rect width="{w}" height="{h}" fill="#fafafa"/>\n'
    svg += f'<text x="{w/2}" y="28" text-anchor="middle" font-family="sans-serif" font-size="15" font-weight="bold" fill="#212121">Operators Architecture: Controller -> Provisioner -> External System</text>\n'

    def box(x, y, bw, bh, label, color="#1565c0", text_color="white", font_size=11):
        lines = label.split('\n')
        result = f'<rect x="{x}" y="{y}" width="{bw}" height="{bh}" fill="{color}" rx="6" stroke="#ccc" stroke-width="1"/>\n'
        for i, line in enumerate(lines):
            ty = y + bh/2 - (len(lines)-1)*7 + i*14
            result += f'<text x="{x+bw/2}" y="{ty}" text-anchor="middle" font-family="sans-serif" font-size="{font_size}" fill="{text_color}">{line}</text>\n'
        return result

    def arrow(x1, y1, x2, y2, label="", dashed=False):
        dash = ' stroke-dasharray="5,3"' if dashed else ''
        result = f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="#546e7a" stroke-width="1.5"{dash} marker-end="url(#arr)"/>\n'
        if label:
            mx, my = (x1+x2)/2, (y1+y2)/2
            result += f'<text x="{mx}" y="{my-4}" text-anchor="middle" font-family="sans-serif" font-size="9" fill="#546e7a">{label}</text>\n'
        return result

    # Arrow marker
    svg += '''<defs><marker id="arr" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L0,6 L8,3 z" fill="#546e7a"/></marker></defs>\n'''

    # Layer 1: Controllers (left column)
    controllers = [
        ("TenantController", 30, 55),
        ("EnvironmentController", 30, 120),
        ("PlatformBootstrapController", 30, 185),
        ("ApplicationController", 30, 250),
        ("ExposureController", 30, 315),
        ("GitRepoBindingController", 30, 380),
        ("ClusterCredentialController", 30, 445),
    ]
    for name, x, y in controllers:
        color = "#e53935" if name in ("PreviewController",) else "#1565c0"
        svg += box(x, y, 175, 32, name, color="#1565c0", font_size=10)

    # Layer 2: Provisioners (middle)
    provisioners = [
        ("Gitea\nProvisioner\n(CB wrapped)", 255, 55),
        ("ArgoCD\nProvisioner\n(CB wrapped)", 255, 160),
        ("MinIO\nProvisioner\n(!CB UNWRAPPED)", 255, 265, "#e53935"),
        ("Harbor\nProvisioner\n(CB wrapped)", 255, 370),
    ]
    for item in provisioners:
        if len(item) == 4:
            name, x, y, color = item
        else:
            name, x, y = item
            color = "#2e7d32"
        svg += box(x, y, 130, 50, name, color=color, font_size=9)

    # Layer 3: External Systems (right)
    externals = [
        ("Gitea\n(GitOps)", 445, 55, "#0288d1"),
        ("ArgoCD\n(Sync)", 445, 160, "#0288d1"),
        ("MinIO\n(Builds)", 445, 265, "#0288d1"),
        ("Harbor\n(Registry)", 445, 370, "#0288d1"),
        ("K8s API\n(mgmt/build/runtime)", 445, 430, "#37474f"),
    ]
    for name, x, y, color in externals:
        svg += box(x, y, 110, 45, name, color=color, font_size=10)

    # Arrows: controllers -> provisioners
    svg += arrow(205, 71, 255, 80, "")
    svg += arrow(205, 136, 255, 185, "")
    svg += arrow(205, 201, 255, 185, "")
    svg += arrow(205, 71, 255, 80)
    # TenantController -> all provisioners
    svg += arrow(205, 71, 255, 80)
    svg += arrow(205, 71, 255, 185)
    svg += arrow(205, 71, 255, 290)
    svg += arrow(205, 71, 255, 395)
    # PlatformBootstrap -> Gitea + ArgoCD
    svg += arrow(205, 201, 255, 80)
    svg += arrow(205, 201, 255, 185)

    # Provisioners -> externals
    svg += arrow(385, 80, 445, 77)
    svg += arrow(385, 185, 445, 182)
    svg += arrow(385, 290, 445, 287)
    svg += arrow(385, 395, 445, 392)

    # Controllers -> K8s API directly
    svg += arrow(205, 136, 445, 445, "", dashed=True)
    svg += arrow(205, 266, 445, 445, "", dashed=True)

    # Legend
    svg += box(560, 55, 140, 22, "CB wrapped provisioner", color="#2e7d32", font_size=9)
    svg += box(560, 85, 140, 22, "UNWRAPPED (bug)", color="#e53935", font_size=9)
    svg += box(560, 115, 140, 22, "External system", color="#0288d1", font_size=9)
    svg += f'<line x1="560" y1="148" x2="700" y2="148" stroke="#546e7a" stroke-width="1.5" stroke-dasharray="5,3"/>\n'
    svg += f'<text x="630" y="162" text-anchor="middle" font-family="sans-serif" font-size="9" fill="#546e7a">Direct K8s API call</text>\n'

    svg += '</svg>'
    return svg


if __name__ == "__main__":
    out = Path("/home/paulo/loopzero/review-output/figures")
    out.mkdir(parents=True, exist_ok=True)

    figures = {
        "findings_by_severity.svg": generate_findings_by_severity(),
        "findings_by_phase.svg": generate_findings_by_phase(),
        "findings_by_category.svg": generate_findings_by_category(),
        "risk_matrix.svg": generate_risk_matrix(),
        "dependency_graph.svg": generate_dependency_graph(),
    }

    for filename, content in figures.items():
        path = out / filename
        path.write_text(content, encoding="utf-8")
        print(f"Generated: {path}")

    print("All figures generated successfully.")
