# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
"""Parsing and formatting amounts as integer cents."""


def parse_amount(text: str) -> int:
    """Parses "12.34", "12" or "-0.5" into cents."""
    text = text.strip()
    sign = -1 if text.startswith("-") else 1
    text = text.lstrip("+-")
    whole, _, frac = text.partition(".")
    if len(frac) > 2:
        raise ValueError(f"too many decimals: {text!r}")
    frac = (frac + "00")[:2]
    return sign * (int(whole or "0") * 100 + int(frac))


def format_cents(cents: int) -> str:
    """Formats cents as "12.34", with a leading minus when negative."""
    sign = "-" if cents < 0 else ""
    cents = abs(cents)
    return f"{sign}{cents // 100}.{cents % 100:02d}"


def round_half_up(numerator: int, denominator: int) -> int:
    """Divides and rounds halves away from zero."""
    q, r = divmod(abs(numerator), denominator)
    if 2 * r >= denominator:
        q += 1
    return q if numerator >= 0 else -q
