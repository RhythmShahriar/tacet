# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
"""Invoices: lines, a bulk discount on the subtotal, then tax."""

from dataclasses import dataclass, field

from .money import round_half_up

# Orders of at least this many units get BULK_PERCENT off the subtotal.
BULK_UNITS = 10
BULK_PERCENT = 5


@dataclass
class Line:
    sku: str
    unit_cents: int
    quantity: int = 1

    def cents(self) -> int:
        return self.unit_cents * self.quantity


@dataclass
class Invoice:
    lines: list = field(default_factory=list)
    tax_percent: int = 0

    def add(self, sku: str, unit_cents: int, quantity: int = 1) -> "Invoice":
        if quantity <= 0:
            raise ValueError("quantity must be positive")
        self.lines.append(Line(sku, unit_cents, quantity))
        return self

    def units(self) -> int:
        return sum(l.quantity for l in self.lines)

    def subtotal(self) -> int:
        return sum(l.cents() for l in self.lines)

    def discount(self) -> int:
        if self.units() > BULK_UNITS:
            return round_half_up(self.subtotal() * BULK_PERCENT, 100)
        return 0

    def tax(self) -> int:
        return round_half_up((self.subtotal() - self.discount()) * self.tax_percent, 100)

    def total(self) -> int:
        return self.subtotal() - self.discount() + self.tax()
