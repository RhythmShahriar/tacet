#!/usr/bin/env python3
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
"""Turns the plain-text RFC 6749 into HTML with real headings.

LibreOffice maps h1/h2/h3 to the Heading 1/2/3 styles, so the PDF gets
bookmarks and the DOCX gets a real outline. Made-up implementer notes go
into chosen sections so the answers can't come from a model's memory.

Usage: rfc2html.py <rfc6749.txt> <out.html>
"""

import html
import re
import sys

# Section -> note. The bench tasks ask about these; keep check.sh in step.
NOTES = {
    # Start of the document (pdf-whole).
    "1.2": "Implementer note: the reference deployment that follows this guide is code-named HARBOR-LANTERN-17.",
    # Middle (pdf-whole).
    "4.3": "Implementer note: the middle milestone of the reference rollout is code-named COPPER-WILLOW-42.",
    # One part only (pdf-part).
    "4.4": "Implementer note: in the reference deployment, client credentials tokens expire after 38 minutes and are issued only to clients in the ledger-sync group.",
    # One part only (docx).
    "6": "Implementer note: the reference deployment allows each refresh token 9 uses and logs any reuse to the tokens-audit channel.",
    # End of the document (pdf-whole).
    "Appendix B": "Implementer note: the final milestone of the reference rollout is code-named SALT-MERIDIAN-88.",
}

HEADING = re.compile(r"^((?:\d+|Appendix [A-Z])(?:\.\d+)*)\.?  +(\S.*)$")
PAGE_FOOT = re.compile(r"^Hardt\s+Standards Track\s+\[Page \d+\]$")
PAGE_HEAD = re.compile(r"^RFC 6749\s+OAuth 2\.0\s+October 2012$")


def body_lines(text):
    lines = text.replace("\f", "").split("\n")
    # The body starts at the second "1.  Introduction" (the first is in the
    # table of contents, written "1. Introduction").
    start = next(i for i, l in enumerate(lines) if l.startswith("1.  Introduction"))
    out = []
    for l in lines[start:]:
        if PAGE_FOOT.match(l) or PAGE_HEAD.match(l):
            continue
        out.append(l.rstrip())
    return out


def blocks(lines):
    para = []
    for l in lines:
        if not l.strip():
            if para:
                yield para
                para = []
            continue
        if HEADING.match(l) and not para:
            yield [l]
            continue
        para.append(l)
    if para:
        yield para


def indent(l):
    return len(l) - len(l.lstrip(" "))


def render(text):
    out = [
        "<!DOCTYPE html>",
        "<html><head><meta charset=\"utf-8\"><title>The OAuth 2.0 Authorization Framework</title></head><body>",
        "<h1>The OAuth 2.0 Authorization Framework</h1>",
        "<p>RFC 6749, October 2012. Plain text from the RFC Editor, laid out for reading.</p>",
    ]
    pending = None
    for b in blocks(body_lines(text)):
        m = HEADING.match(b[0]) if len(b) == 1 else None
        if m:
            num, title = m.group(1), m.group(2)
            depth = 1 if num.startswith("Appendix") else num.count(".") + 1
            tag = "h%d" % min(depth + 1, 4)
            out.append("<%s>%s %s</%s>" % (tag, html.escape(num + "."), html.escape(title), tag))
            pending = NOTES.get(num)
            continue
        if min(indent(l) for l in b) >= 5 or any(re.search(r"[+|]-|-[+|]|-->|<--", l) for l in b):
            out.append("<pre>%s</pre>" % html.escape("\n".join(b)))
        else:
            out.append("<p>%s</p>" % html.escape(" ".join(l.strip() for l in b)))
        if pending:
            out.append("<p><b>%s</b></p>" % html.escape(pending))
            pending = None
    out.append("</body></html>")
    return "\n".join(out) + "\n"


def main(argv):
    if len(argv) != 2:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    with open(argv[0], encoding="utf-8") as f:
        text = f.read()
    with open(argv[1], "w", encoding="utf-8") as f:
        f.write(render(text))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
