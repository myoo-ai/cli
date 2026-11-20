use shapes_kit::geo::circle::Circle;
use shapes_kit::{total_area, Point};

fn main() {
    let c = Circle { center: Point::new(1.0, 2.0), radius: 3.0 };
    let shifted = c.shifted(1.0);
    println!("{}", total_area(&[Box::new(shifted)]));
}
