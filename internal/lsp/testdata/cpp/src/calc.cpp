#include "calc.h"

namespace shop {

// add adds a and b to the total.
int Calc::add(int a, int b) {
    total += a + b;
    return total;
}

}  // namespace shop
