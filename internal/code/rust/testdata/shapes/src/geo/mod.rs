pub mod point;

/// Anything with an area.
pub trait Shape {
    type Unit;
    const SIDES: u32 = 0;
    fn area(&self) -> f64;
    fn describe(&self) -> String {
        format!("{} sides, area {}", Self::SIDES, self.area())
    }
}

pub mod circle {
    use super::Shape;
    use super::point::Point;

    pub struct Circle {
        pub center: Point,
        pub radius: f64,
    }

    impl Shape for Circle {
        type Unit = f64;
        fn area(&self) -> f64 {
            std::f64::consts::PI * self.radius * self.radius
        }
    }

    impl Circle {
        pub fn shifted(&self, by: f64) -> Circle {
            let c = Circle { center: self.center.moved(by), ..*self };
            c.center.norm();
            c
        }
    }
}
