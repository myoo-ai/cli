use std::ops::Add;

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Point {
    pub x: f64,
    pub y: f64,
}

pub struct Origin(pub f64);

impl Point {
    pub fn new(x: f64, y: f64) -> Self {
        Self { x, y }
    }

    pub fn norm(&self) -> f64 {
        (self.x * self.x + self.y * self.y).sqrt()
    }

    pub fn moved(self, by: f64) -> Point {
        Point::new(self.x + by, self.y + by)
    }
}

impl Add for Point {
    type Output = Point;
    fn add(self, o: Point) -> Point {
        Point { x: self.x + o.x, y: self.y + o.y }
    }
}

unsafe impl Send for Origin {}

pub(crate) const fn origin() -> Point {
    Point { x: 0.0, y: 0.0 }
}

extern "C" {
    fn abs(v: i32) -> i32;
}

pub async unsafe fn raw_abs(v: i32) -> i32 {
    unsafe { abs(v) }
}
