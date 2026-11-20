#pragma once
#include <string>
#include <vector>

namespace geo {

/// Shape is the base of all shapes.
class Shape {
public:
    explicit Shape(std::string name);
    virtual ~Shape() = default;
    virtual double area() const = 0;
    const std::string& name() const { return name_; }
    bool operator<(const Shape& other) const;

protected:
    std::string name_;
};

class Circle : public Shape {
public:
    Circle(double r);
    double area() const override;
    double radius() const { return radius_; }

private:
    double radius_;
};

template <typename T>
class Box {
public:
    explicit Box(T v) : value_(v) {}
    T get() const;
    template <class U>
    U as() const { return static_cast<U>(value_); }

private:
    T value_;
};

template <typename T>
T Box<T>::get() const {
    return value_;
}

enum class Kind { Round, Square };

double total_area(const std::vector<Shape*>& shapes);

}  // namespace geo
