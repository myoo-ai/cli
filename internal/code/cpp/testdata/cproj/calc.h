#ifndef CALC_H
#define CALC_H

#define MAX_ITEMS 16
#define SQUARE(x) ((x) * (x))
#define LOG(fmt, ...) \
    printf(fmt, __VA_ARGS__)

/** Point is a 2D point. */
typedef struct {
    int x;
    int y;
} Point;

typedef struct node {
    int value;
    struct node *next;
} node_t;

enum color { RED, GREEN = 2, BLUE };

extern int counter;

int add(int a, int b);
int scale(Point *p, int factor);
typedef int (*binop)(int, int);

#endif
