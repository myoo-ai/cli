#pragma once
#include <stdlib.h>

#ifdef __cplusplus
extern "C" {
#endif

#define API_VERSION 3

typedef struct handle handle_t;

static inline char *api_alloc(size_t n)
{
    char *p = (char *)malloc(n);
    return p != NULL ? p : NULL;
}

#ifdef __cplusplus
}
#endif
