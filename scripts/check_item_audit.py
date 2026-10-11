#!/usr/bin/env python3
"""Checks a running API against docs/audits/CROC-082-items.md.

  before: the API still matches the doc's Now column, so the migrations will apply cleanly.
  after:  the API matches the doc's Proposed column, renames and merges included.

Usage: scripts/check_item_audit.py before|after [API base URL, default http://localhost:8080]
"""
import json
import re
import sys
import urllib.parse
import urllib.request
from pathlib import Path

DOC = Path(__file__).resolve().parent.parent / "docs" / "audits" / "CROC-082-items.md"


def parse_doc():
    rows, typos, excluded, renamed = [], {}, set(), {}
    section = None
    for line in DOC.read_text().splitlines():
        if line.startswith("## "):
            section = line[3:]
            continue
        if not line.startswith("| ") or line.startswith("| ---"):
            continue
        cells = [c.strip() for c in line.strip("|").split("|")]
        if cells[0] in ("Item", "Category", "Now"):
            continue
        if section == "Category totals":
            name, tags = re.match(r"(.+?)(?: \((.*)\))?$", cells[0]).groups()
            for tag in (tags or "").split(", "):
                if tag == "excluded":
                    excluded.add(name)
                elif tag.startswith("renamed from "):
                    renamed[tag.removeprefix("renamed from ")] = name
        elif section == "Typo fixes":
            typos[cells[0]] = cells[1]
        elif section != "Merges and deletions":
            item, _, now, proposed, _ = cells
            rows.append((item, now, proposed.strip("*")))
    return rows, typos, excluded, renamed


def get(api, path):
    # Cloudflare refuses urllib's default User-Agent.
    req = urllib.request.Request(f"{api}{path}", headers={"User-Agent": "crockpot-item-audit"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)


def recipe_ids(api, item_id):
    ids, page = set(), 1
    while True:
        body = get(api, f"/recipes?limit=50&page={page}&ingredientId={item_id}")
        ids |= {r["id"] for r in body["recipes"]}
        if page >= body["totalPages"]:
            return ids
        page += 1


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in ("before", "after"):
        sys.exit(__doc__)
    stage = sys.argv[1]
    api = (sys.argv[2] if len(sys.argv) > 2 else "http://localhost:8080").rstrip("/")

    rows, typos, excluded, renamed = parse_doc()
    cats = get(api, "/item-categories")
    cat_name = {c["id"]: c["name"] for c in cats}
    live = {i["name"]: i for i in get(api, "/items")}
    problems = []

    if stage == "before":
        for item, now, proposed in rows:
            if item not in live:
                problems.append(f"missing item: {item}")
            elif cat_name[live[item]["categoryId"]] != now:
                problems.append(f"{item}: is in {cat_name[live[item]['categoryId']]}, doc says {now}")
            elif proposed.startswith("merge into ") and proposed.removeprefix("merge into ") not in live:
                problems.append(f"{item}: merge target missing")
        for old, new in typos.items():
            if new in live:
                problems.append(f"rename target already taken: {new}")
        doc_items = {r[0] for r in rows}
        for name in sorted(set(live) - doc_items):
            problems.append(f"item not in the doc: {name}")
        for item, _, proposed in rows:
            if proposed in excluded and item in live:
                found = get(api, f"/recipes?limit=1&ingredientId={live[item]['id']}")["total"]
                if found:
                    problems.append(f"{item}: bound for excluded {proposed} but used in {found} recipe(s)")
        for item, _, proposed in rows:
            kept = proposed.removeprefix("merge into ")
            if kept != proposed and item in live and kept in live:
                both = recipe_ids(api, live[item]["id"]) & recipe_ids(api, live[kept]["id"])
                if both:
                    problems.append(f"{item}: {len(both)} recipe(s) also use {kept}, so the merge would break")
    else:
        flags = {c["name"]: c["isIngredient"] for c in cats}
        for old in renamed:
            if old in flags:
                problems.append(f"category not renamed: {old}")
        for item, now, proposed in rows:
            name = typos.get(item, item)
            if proposed == "delete" or proposed.startswith("merge into "):
                if item in live:
                    problems.append(f"{item}: should be gone ({proposed})")
                continue
            if name not in live:
                problems.append(f"missing item: {name}")
                continue
            got = cat_name[live[name]["categoryId"]]
            if got != proposed:
                problems.append(f"{name}: is in {got}, doc says {proposed}")
        for cat in {r[2] for r in rows if r[2] != "delete" and not r[2].startswith("merge into ")}:
            if cat not in flags:
                problems.append(f"missing category: {cat}")
            elif flags[cat] == (cat in excluded):
                problems.append(f"{cat}: isIngredient is {flags[cat]}")

    for p in problems:
        print(p)
    print(f"{stage}: {len(rows)} doc rows, {len(live)} live items, {len(problems)} problem(s)")
    sys.exit(1 if problems else 0)


if __name__ == "__main__":
    main()
