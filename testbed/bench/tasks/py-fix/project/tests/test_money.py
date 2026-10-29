# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
import pytest

from tally.money import format_cents, parse_amount, round_half_up


def test_parse_amount():
    assert parse_amount("12.34") == 1234
    assert parse_amount("12") == 1200
    assert parse_amount("-0.5") == -50
    assert parse_amount(" 3.07 ") == 307


def test_parse_amount_rejects_three_decimals():
    with pytest.raises(ValueError):
        parse_amount("1.234")


def test_format_cents():
    assert format_cents(1234) == "12.34"
    assert format_cents(5) == "0.05"
    assert format_cents(-250) == "-2.50"


def test_round_half_up():
    assert round_half_up(250, 100) == 3
    assert round_half_up(249, 100) == 2
    assert round_half_up(-250, 100) == -3
