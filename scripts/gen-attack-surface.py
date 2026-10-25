#!/usr/bin/env python3
"""Writes the attack-surface tables of doc/security/audit-package.md.

The routes come from api/openapi.json (a test, TestOpenAPICoversRoutes, fails
when a registered route is missing there), the WebSocket events from the
registry golden file. Run it again after the routes change:

    python3 scripts/gen-attack-surface.py           # rewrite the tables in place
    python3 scripts/gen-attack-surface.py --check   # exit 1 when the document is stale
"""
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DOC = ROOT / "doc" / "security" / "audit-package.md"
BEGIN = "<!-- attack-surface:begin -->"
END = "<!-- attack-surface:end -->"
METHODS = ("get", "post", "put", "patch", "delete")


def gate(path: str, security: list) -> str:
    """The authentication a route asks for, derived from its security scheme and mount point."""
    if path == "/api/ws":
        return "Session cookie on upgrade, origin check"
    if path.startswith("/api/webhooks/") and not security:
        return "Secret in URL"
    if not security:
        return "None (public)"
    scheme = next(iter(security[0]))
    if scheme == "botAuth":
        return "Bot token"
    if scheme == "bearerAuth":
        return "Bearer METRICS_TOKEN"
    if path.startswith("/api/admin"):
        return "Session + admin-area permission"
    return "Session"


def cell(text: str) -> str:
    return re.sub(r"\s+", " ", text).replace("|", "\\|").strip()


def routes_section(spec: dict) -> str:
    groups = defaultdict(list)
    count = 0
    for path, item in sorted(spec["paths"].items()):
        for method in METHODS:
            op = item.get(method)
            if not op:
                continue
            count += 1
            tag = (op.get("tags") or ["Other"])[0]
            groups[tag].append((method.upper(), path, gate(path, op.get("security", [])), op.get("summary", "")))
    out = [f"{count} operations on {len(spec['paths'])} paths, grouped by API tag.\n"]
    for tag in sorted(groups):
        out.append(f"\n#### {tag} ({len(groups[tag])})\n")
        out.append("| Method | Path | Authentication | Summary |")
        out.append("|---|---|---|---|")
        for method, path, auth, summary in groups[tag]:
            out.append(f"| {method} | `{path}` | {cell(auth)} | {cell(summary)} |")
    return "\n".join(out)


def events_section(golden: dict) -> str:
    out = []
    for side, title in (("server", "Server to client"), ("client", "Client to server")):
        names = sorted(golden[side].values())
        out.append(f"\n**{title} ({len(names)}):** " + ", ".join(f"`{n}`" for n in names) + "\n")
    return "\n".join(out)


def render() -> str:
    spec = json.loads((ROOT / "api" / "openapi.json").read_text())
    golden = json.loads((ROOT / "internal" / "events" / "testdata" / "events.golden.json").read_text())
    return "\n".join([
        BEGIN,
        "",
        "### REST routes",
        "",
        routes_section(spec),
        "",
        "### WebSocket events",
        events_section(golden),
        END,
    ])


def main() -> int:
    text = DOC.read_text()
    start, end = text.index(BEGIN), text.index(END) + len(END)
    updated = text[:start] + render() + text[end:]
    if "--check" in sys.argv:
        if updated != text:
            print("doc/security/audit-package.md: attack surface is stale; run scripts/gen-attack-surface.py")
            return 1
        return 0
    DOC.write_text(updated)
    return 0


if __name__ == "__main__":
    sys.exit(main())
