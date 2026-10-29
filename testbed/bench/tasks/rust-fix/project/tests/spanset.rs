// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

use spans::{Span, SpanSet};

fn sp(a: i64, b: i64) -> Span {
    Span::new(a, b).unwrap()
}

#[test]
fn rejects_empty_spans() {
    assert!(Span::new(3, 3).is_none());
    assert!(Span::new(5, 1).is_none());
}

#[test]
fn keeps_disjoint_spans_sorted() {
    let mut set = SpanSet::new();
    set.insert(sp(10, 12));
    set.insert(sp(1, 3));
    assert_eq!(set.spans(), &[sp(1, 3), sp(10, 12)]);
    assert_eq!(set.covered(), 4);
}

#[test]
fn merges_overlapping_spans() {
    let mut set = SpanSet::new();
    set.insert(sp(1, 5));
    set.insert(sp(4, 9));
    assert_eq!(set.spans(), &[sp(1, 9)]);
}

#[test]
fn merges_touching_spans() {
    let mut set = SpanSet::new();
    set.insert(sp(1, 4));
    set.insert(sp(4, 6));
    assert_eq!(set.spans(), &[sp(1, 6)]);
    assert!(set.contains(4));
    assert!(!set.contains(6));
}
