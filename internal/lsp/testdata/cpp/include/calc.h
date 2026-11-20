#pragma once

namespace shop {

// Calc accumulates a total.
class Calc {
public:
    int add(int a, int b);

private:
    int total = 0;
};

}  // namespace shop
