# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
import pytest

from tally import Invoice


def test_small_order_has_no_discount():
    inv = Invoice(tax_percent=10).add("pen", 150, 4)
    assert inv.subtotal() == 600
    assert inv.discount() == 0
    assert inv.total() == 660


def test_bulk_discount_starts_at_ten_units():
    inv = Invoice().add("pen", 100, 6).add("pad", 250, 4)
    assert inv.units() == 10
    assert inv.discount() == 80
    assert inv.total() == 1520


def test_tax_is_on_the_discounted_subtotal():
    inv = Invoice(tax_percent=20).add("ink", 1000, 12)
    assert inv.discount() == 600
    assert inv.tax() == 2280
    assert inv.total() == 13680


def test_quantity_must_be_positive():
    with pytest.raises(ValueError):
        Invoice().add("pen", 100, 0)
