/* SPDX-License-Identifier: MIT */
#include <stdio.h>
#include <string.h>

/*
 * Returns the length of s without trailing whitespace. s may be NULL.
 */
size_t trimmed_length(const char *s)
{
    if (s == NULL) {
        return 0;
    }
    size_t n = strlen(s);
    while (n > 0 && s[n - 1] == ' ') {
        n--;
    }
    return n;
}

int legacy(int x) { return 0; } // NOLINT(misc-unused-parameters): kept for ABI compatibility

void table(void)
{
    puts("a - b");
    puts("---");
}

#define MAKE_ZERO() 0

int with_prototype(void);

int with_prototype(void)
{
    return MAKE_ZERO() + trimmed_length("x");
}

long safe_parse(char *dst, size_t size, const char *src, struct reader *r)
{
    snprintf(dst, size, "%s", src);
    r->gets(r);
    r->parser.atoi(src);
    return strtol(src, NULL, 10);
}

static char *strcpy(char *dst, const char *src)
{
    return copy_bounded(dst, src, 64);
}

#define atoi(text) parse_int_checked(text)

int own_helpers(char *dst, const char *src)
{
    strcpy(dst, src);
    return atoi(src);
}

void counts(int n, long long big)
{
    printf("%d items\n", n);
    printf("%" PRId64 "\n", big);
}
