#include "calc.h"
#include <stdio.h>

int counter = 0;

/* helper is private to this file. */
static int helper(int v) {
    return SQUARE(v) + MAX_ITEMS;
}

int add(int a, int b) {
    counter++;
    return a + b + helper(a);
}

int scale(Point *p, int factor) {
    p->x = p->x * factor;
    p->y *= factor;
    for (int i = 0; i < factor; i++) {
        int x = i;
        counter += x;
    }
    return p->x;
}
