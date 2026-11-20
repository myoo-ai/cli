/*
 * positive.c - demo helpers.
 * Shows every rule.
 */
#include <stdio.h>
#include <stdlib.h>

/* ********************************** */

/* This is a function that gets the user. */
const char *get_user(const char *id)
{
    /* Step 1: Read the environment variable */
    const char *user = getenv(id);
    // Now we return the user to the caller
    return user;
}

int count(int counter)
{
    // Increment the counter variable here
    counter++;
    return counter;
}

void steps(char *text)
{
    // 1. Trim the whitespace off the text
    trim(text);
    // 2. Print the trimmed text to stdout
    puts(text);
}

void todo(void)
{
    // TODO
}

void report(void)
{
    printf("✅ done\n");
    printf("a → b\n");
    printf("x — y\n");
    printf("==========\n");
}

#pragma GCC diagnostic ignored "-Wunused"
int unused(int x) { return 0; } // NOLINT

int chatty(int x)
{
    // This explanation starts here
    // and keeps going
    // for a while
    // without saying much
    // about anything
    // in particular
    return x;
}

int nested(int *a, int n)
{
    for (int i = 0; i < n; i++) {
        if (a[i] > 0) {
            while (a[i] > 1) {
                switch (a[i]) {
                case 2:
                    if (n > 3) {
                        return a[i];
                    }
                }
                a[i]--;
            }
        }
    }
    return 0;
}

int long_function(void)
{
    int total = 0;
    total += 0;
    total += 1;
    total += 2;
    total += 3;
    total += 4;
    total += 5;
    total += 6;
    total += 7;
    total += 8;
    total += 9;
    total += 10;
    total += 11;
    total += 12;
    total += 13;
    total += 14;
    total += 15;
    total += 16;
    total += 17;
    total += 18;
    total += 19;
    total += 20;
    total += 21;
    total += 22;
    total += 23;
    total += 24;
    total += 25;
    total += 26;
    total += 27;
    total += 28;
    total += 29;
    total += 30;
    total += 31;
    total += 32;
    total += 33;
    total += 34;
    total += 35;
    total += 36;
    total += 37;
    total += 38;
    total += 39;
    total += 40;
    total += 41;
    total += 42;
    total += 43;
    total += 44;
    total += 45;
    total += 46;
    total += 47;
    total += 48;
    total += 49;
    total += 50;
    total += 51;
    total += 52;
    total += 53;
    total += 54;
    total += 55;
    total += 56;
    total += 57;
    total += 58;
    total += 59;
    total += 60;
    total += 61;
    total += 62;
    total += 63;
    total += 64;
    total += 65;
    total += 66;
    total += 67;
    total += 68;
    total += 69;
    total += 70;
    total += 71;
    total += 72;
    total += 73;
    total += 74;
    total += 75;
    total += 76;
    total += 77;
    total += 78;
    total += 79;
    total += 80;
    total += 81;
    return total;
}

int copy_one(struct row *rows, int n, int limit)
{
    int result = 0;
    for (int i = 0; i < n; i++) {
        if (rows[i].size > limit && rows[i].name[0] == 'a') {
            result += rows[i].size * 2 + rows[i].owner;
        } else if (rows[i].size < 0) {
            fprintf(stderr, "negative size %s\n", rows[i].name);
        }
    }
    return result;
}

int copy_two(struct item *items, int count, int maximum)
{
    int found = 0;
    for (int k = 0; k < count; k++) {
        if (items[k].weight > maximum && items[k].label[0] == 'b') {
            found += items[k].weight * 3 + items[k].parent;
        } else if (items[k].weight < 0) {
            fprintf(stderr, "negative weight %s\n", items[k].label);
        }
    }
    return found;
}

void unsafe_copy(char *dst, const char *src)
{
    char line[64];
    gets(line);
    strcpy(dst, src);
    strcat(dst, line);
    sprintf(dst, "%s", src);
}

int parse_number(const char *text)
{
    return atoi(text) + (int)atof(text);
}

int no_prototype();

int no_prototype()
{
    return 1;
}
