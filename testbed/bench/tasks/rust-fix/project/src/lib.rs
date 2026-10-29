// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

//! A set of half-open ranges `[start, end)` kept sorted and merged.

/// One half-open range.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Span {
    pub start: i64,
    pub end: i64,
}

impl Span {
    /// A span; an empty or reversed one is rejected.
    pub fn new(start: i64, end: i64) -> Option<Span> {
        if start < end {
            Some(Span { start, end })
        } else {
            None
        }
    }

    pub fn len(&self) -> i64 {
        self.end - self.start
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    pub fn contains(&self, x: i64) -> bool {
        self.start <= x && x < self.end
    }
}

/// Sorted, merged spans.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct SpanSet {
    spans: Vec<Span>,
}

impl SpanSet {
    pub fn new() -> SpanSet {
        SpanSet::default()
    }

    /// Adds a span, merging it with every span it overlaps or touches.
    pub fn insert(&mut self, s: Span) {
        self.spans.push(s);
        self.spans.sort_by_key(|s| s.start);
        let mut merged: Vec<Span> = Vec::with_capacity(self.spans.len());
        for s in self.spans.drain(..) {
            match merged.last_mut() {
                Some(last) if s.start < last.end => last.end = last.end.max(s.end),
                _ => merged.push(s),
            }
        }
        self.spans = merged;
    }

    pub fn contains(&self, x: i64) -> bool {
        self.spans.iter().any(|s| s.contains(x))
    }

    /// The total length covered.
    pub fn covered(&self) -> i64 {
        self.spans.iter().map(Span::len).sum()
    }

    pub fn spans(&self) -> &[Span] {
        &self.spans
    }
}
