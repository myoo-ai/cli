#include "calc.h"

int main() {
    shop::Calc calc;
    int sum = calc.add(1, 2);
    return calc.add(sum, 3);
}
