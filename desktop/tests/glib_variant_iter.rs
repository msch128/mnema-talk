//! Regression for the GTK3 GLib backport, run against real GLib in release mode.
#![cfg(target_os = "linux")]

use glib::prelude::*;

const WORDS: [&str; 6] = ["", "alpha", "Grüße", "日本語", "🎙️", "omega"];

#[test]
fn next_and_exhaustion_preserve_borrowed_utf8() {
    let variant = WORDS.to_variant();
    let mut iter = variant.array_iter_str().unwrap();
    for (index, word) in WORDS.iter().enumerate() {
        assert_eq!(iter.len(), WORDS.len() - index);
        assert_eq!(iter.next(), Some(*word));
    }
    assert_eq!(iter.size_hint(), (0, Some(0)));
    assert_eq!(iter.next(), None);
    assert_eq!(iter.next_back(), None);
    assert_eq!(iter.next(), None);
}

#[test]
fn nth_and_last_use_real_out_pointer() {
    let variant = WORDS.to_variant();
    let mut iter = variant.array_iter_str().unwrap();
    assert_eq!(iter.nth(2), Some(WORDS[2]));
    assert_eq!(iter.next(), Some(WORDS[3]));
    assert_eq!(iter.last(), Some(WORDS[5]));
    assert_eq!(variant.array_iter_str().unwrap().last(), Some(WORDS[5]));
}

#[test]
fn backwards_and_mixed_iteration_do_not_cross() {
    let variant = WORDS.to_variant();
    let mut iter = variant.array_iter_str().unwrap();
    assert_eq!(iter.next_back(), Some(WORDS[5]));
    assert_eq!(iter.nth_back(1), Some(WORDS[3]));
    assert_eq!(iter.next(), Some(WORDS[0]));
    assert_eq!(iter.next_back(), Some(WORDS[2]));
    assert_eq!(iter.next(), Some(WORDS[1]));
    assert_eq!(iter.len(), 0);
    assert_eq!(iter.next_back(), None);
    assert_eq!(iter.next(), None);
}

#[test]
fn empty_and_overflowing_skips_stay_exhausted() {
    let variant = Vec::<String>::new().to_variant();
    assert_eq!(variant.array_iter_str().unwrap().next(), None);
    assert_eq!(variant.array_iter_str().unwrap().last(), None);
    let variant = WORDS.to_variant();
    let mut forwards = variant.array_iter_str().unwrap();
    assert_eq!(forwards.nth(usize::MAX), None);
    assert_eq!(forwards.next_back(), None);
    let mut backwards = variant.array_iter_str().unwrap();
    assert_eq!(backwards.nth_back(usize::MAX), None);
    assert_eq!(backwards.next(), None);
}

#[test]
fn repeated_full_traversals_preserve_all_items() {
    let variant = WORDS.to_variant();
    for _ in 0..1024 {
        assert_eq!(variant.array_iter_str().unwrap().collect::<Vec<_>>(), WORDS);
        assert_eq!(
            variant.array_iter_str().unwrap().rev().collect::<Vec<_>>(),
            WORDS.into_iter().rev().collect::<Vec<_>>()
        );
    }
}
