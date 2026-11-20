#include "geo/shape.h"
#include <memory>

using namespace geo;

namespace app::util {
inline int clamp(int v, int lo, int hi) { return v < lo ? lo : v > hi ? hi : v; }
}  // namespace app::util

struct Counter {
    static int count;
    int operator()(int step) { return count += step; }
};

int Counter::count = 0;

int main() {
    auto c = std::make_unique<Circle>(2.0);
    Circle plain(1.5);
    std::vector<Shape*> shapes{c.get(), &plain};
    double a = total_area(shapes) + plain.area();
    Box<int> box(3);
    std::vector<std::vector<int>> grid;
    int v = box.get() + app::util::clamp(1'000, 0, 10);
    Counter counter;
    counter(v);
    auto [lo, hi] = std::pair<int, int>{1, 2};
    const char* raw = R"json({"a": ")"})json";
    auto add = [](int x, int y) { return x + y; };
    Kind k = Kind::Round;
    return add(lo, hi) + static_cast<int>(a) + c->radius() + (k == Kind::Square);
}
