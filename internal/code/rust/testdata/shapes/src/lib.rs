//! Geometry primitives for the shapes kit.
#![allow(dead_code, reason = "fixture")]

pub mod geo;
mod util;
#[path = "extra/strings.rs"]
mod text;

pub use geo::point::{Point, Origin as Zero};
use crate::geo::{self, Shape};
use std::fmt::{self, Display};

/* An outer /* nested */ block comment. */
/** Documents the area function. */
pub fn total_area(shapes: &[Box<dyn Shape>]) -> f64 {
    shapes.iter().map(|s| s.area()).sum()
}

/// A wrapper with a lifetime.
pub struct Labeled<'a, T: Display + 'a> {
    pub label: &'a str,
    pub value: T,
}

impl<'a, T: Display> Labeled<'a, T> {
    pub const KIND: &'static str = "labeled";

    pub fn new(label: &'a str, value: T) -> Self {
        Labeled { label, value }
    }

    pub fn render(&self) -> String {
        let r#type = Self::KIND;
        format!("{}: {} ({})", self.label, self.value, r#type)
    }
}

impl<T: Display> Display for Labeled<'_, T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.render())
    }
}

pub enum Event {
    Click { x: i32, y: i32 },
    Key(char),
    Quit,
}

pub fn describe(e: &Event) -> String {
    match e {
        Event::Click { x, y: row } if *x > 0 => format!("{x} {row}"),
        Event::Key(c @ 'a'..='z') => c.to_string(),
        Event::Key(_) | Event::Quit => String::from(r#"raw "quoted""#),
    }
}

pub fn shadow(input: Option<u32>) -> u32 {
    let x = 1_000u32;
    let x = x + 0x_FF;
    let Some(v) = input else { return x };
    if let Some(w) = input.filter(|n| *n > 2) {
        return w + v;
    }
    'outer: for (i, item) in [1u32, 2].iter().enumerate() {
        while let Some(k) = input {
            if k == *item {
                break 'outer;
            }
            let _ = i;
            break;
        }
    }
    let add = move |a: u32, b| a + b + x;
    add(v, 1)
}

macro_rules! square {
    ($e:expr) => {
        $e * $e
    };
}

pub union Bits {
    pub i: u32,
    pub f: f32,
}

pub static mut COUNTER: u64 = 0;
pub type Area = f64;

pub fn squared(p: &Point) -> Area {
    let z = Zero;
    let q = Point::new(p.x, p.y);
    square!(q.norm()) + util::helper(z.0) + text::shout("a").len() as f64
}

extern crate core as kernel;
use self::geo::circle::*;

pub fn largest<T>(items: &[T]) -> Option<&T>
where
    T: PartialOrd + kernel::fmt::Debug,
{
    let mut best = items.first()?;
    for it in items {
        if it > best {
            best = it;
        }
    }
    let _unit: Circle = todo!();
    Some(best)
}
