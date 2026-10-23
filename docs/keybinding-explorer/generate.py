#!/usr/bin/env python3
"""Build docs/keybinding-explorer.html from trace.json, template.html and live Go source.

Usage (from the repository root):
    python3 docs/keybinding-explorer/generate.py          # validate + build
    python3 docs/keybinding-explorer/generate.py --pin    # (re)record source SHA256 values in trace.json

trace.json is declarative: per flow, ordered steps naming a source file, an
inclusive line range, a highlighted sub-range and `expect` substrings that must
appear on the highlighted lines. The build FAILS (nothing is written) when a
file's SHA256 differs from the pin, a file/range/highlight is invalid, or an
expected substring is missing, so a stale spec can never embed a wrong
highlight. After intentionally changing Go source, fix the ranges in
trace.json, then run with --pin to accept the new hashes.

Only the Python standard library is used. This is a source snapshot, not a
runtime trace.
"""
import hashlib
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
TRACE = HERE / "trace.json"
TEMPLATE = HERE / "template.html"
OUTPUT = HERE.parent / "keybinding-explorer.html"
TOKEN = "__TRACE_DATA__"
FLOW_IDS = ("tab", "modal", "sequence")
STEP_KEYS = ("id", "title", "module", "detail", "file", "start", "end", "highlight", "edge", "expect")


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fail(msg):
    sys.exit("keybinding-explorer: " + msg)


def pin(spec):
    for f in spec["files"]:
        f["sha256"] = sha256(ROOT / f["path"])
    TRACE.write_text(json.dumps(spec, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print("pinned %d file hashes in %s" % (len(spec["files"]), TRACE))


def build(spec):
    if spec.get("schema") != 1:
        fail("trace.json schema must be 1")
    files, lines = [], {}
    for f in spec["files"]:
        p = ROOT / f["path"]
        if not p.is_file():
            fail("missing source file " + f["path"])
        actual = sha256(p)
        if not f.get("sha256"):
            fail("no sha256 pinned for %s; run with --pin after reviewing ranges" % f["path"])
        if f["sha256"] != actual:
            fail("STALE: %s changed (pinned %s.., now %s..). Re-check ranges, then run --pin."
                 % (f["path"], f["sha256"][:12], actual[:12]))
        files.append({"path": f["path"], "sha256": actual})
        lines[f["path"]] = p.read_text(encoding="utf-8").split("\n")
    flows, ids = [], [fl["id"] for fl in spec["flows"]]
    if sorted(ids) != sorted(FLOW_IDS):
        fail("flows must be exactly %s, got %s" % (list(FLOW_IDS), ids))
    for fl in spec["flows"]:
        if not 6 <= len(fl["steps"]) <= 12:
            fail("flow %s needs 6-12 steps" % fl["id"])
        seen, steps = set(), []
        for s in fl["steps"]:
            where = "%s/%s" % (fl["id"], s.get("id"))
            missing = [k for k in STEP_KEYS if k not in s]
            if missing:
                fail("%s missing keys %s" % (where, missing))
            if s["id"] in seen:
                fail("duplicate step id " + where)
            seen.add(s["id"])
            src = lines.get(s["file"])
            if src is None:
                fail("%s uses unlisted file %s" % (where, s["file"]))
            a, b = s["start"], s["end"]
            h0, h1 = s["highlight"]
            if not (1 <= a <= b <= len(src)):
                fail("%s range %d-%d outside %s (%d lines)" % (where, a, b, s["file"], len(src)))
            if not (a <= h0 <= h1 <= b):
                fail("%s highlight %d-%d outside range %d-%d" % (where, h0, h1, a, b))
            hl = "\n".join(src[h0 - 1:h1])
            for needle in s["expect"]:
                if needle not in hl:
                    fail("%s: %r not found on %s lines %d-%d" % (where, needle, s["file"], h0, h1))
            out = {k: s[k] for k in STEP_KEYS if k != "expect"}
            out["code"] = "\n".join(src[a - 1:b])
            steps.append(out)
        flows.append({"id": fl["id"], "label": fl["label"], "description": fl["description"], "steps": steps})
    digest = hashlib.sha256()
    for f in sorted(files, key=lambda x: x["path"]):
        digest.update(("%s:%s\n" % (f["path"], f["sha256"])).encode())
    flows.sort(key=lambda fl: FLOW_IDS.index(fl["id"]))
    return {"schema": 1, "sourceHash": digest.hexdigest(), "files": files, "flows": flows}


def main():
    spec = json.loads(TRACE.read_text(encoding="utf-8"))
    if "--pin" in sys.argv[1:]:
        return pin(spec)
    data = build(spec)
    template = TEMPLATE.read_text(encoding="utf-8")
    if template.count(TOKEN) != 1:
        fail("template must contain %s exactly once" % TOKEN)
    # ensure_ascii escapes U+2028/9; '<', '>' and '&' are escaped so source can
    # never close the <script> element or open a comment.
    payload = json.dumps(data, ensure_ascii=True, separators=(",", ":"))
    payload = payload.replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
    OUTPUT.write_text(template.replace(TOKEN, payload), encoding="utf-8")
    print("wrote %s (sourceHash %s)" % (OUTPUT, data["sourceHash"][:12]))


if __name__ == "__main__":
    main()
