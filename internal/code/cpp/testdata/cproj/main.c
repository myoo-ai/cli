#include "calc.h"
#include <stdio.h>

static int helper(void) {
    return 1;
}

static const char *names[] = {"a", "b"};

int main(int argc, char **argv) {
    Point pt = {1, 2};
    node_t n;
    n.value = add(pt.x, helper());
    n.next = 0;
    binop op = add;
    int total = op(1, 2), *ptr = &total;
    enum color c = GREEN;
    {
        int total = 5;
        LOG("%d %d\n", total, *ptr);
    }
    // Strings with prefixes and escapes.
    const char *s = u8"x";
    char q = '\'';
    printf("%s %c %d %s\n", s, q, c, names[0]);
    return scale(&pt, argc) + total;
}
