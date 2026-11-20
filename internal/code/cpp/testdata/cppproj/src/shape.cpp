#include "geo/shape.h"

#include <algorithm>

namespace geo {

namespace {
constexpr double kPi = 3.14159;
}

Shape::Shape(std::string name) : name_(std::move(name)) {}

bool Shape::operator<(const Shape& other) const {
    return area() < other.area();
}

Circle::Circle(double r) : Shape("circle"), radius_(r) {}

double Circle::area() const {
    return kPi * radius_ * this->radius_;
}

double total_area(const std::vector<Shape*>& shapes) {
    double sum = 0;
    for (const auto* s : shapes) {
        sum += s->area();
    }
    auto twice = [&sum](double x) { return sum * x; };
    return twice(1.0);
}

}  // namespace geo
