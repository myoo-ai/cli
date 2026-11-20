// positive.cpp - demo helpers.
// Shows every rule.
#include <iostream>
#include <stdexcept>
#include <string>

namespace demo {

// ===== Helpers =====

/// This is a function that gets the user.
std::string getUser(const std::string &id)
{
    // Step 1: Read the environment variable
    std::string user = std::getenv(id.c_str());
    // Here we return the user to the caller
    return user;
}

int count(int counter)
{
    // Increment the counter variable here
    counter++;
    return counter;
}

void steps(std::string text)
{
    // First, trim the whitespace off the text
    trim(text);
    // Finally, print the text to stdout
    std::cout << text;
}

int placeholder()
{
    throw std::logic_error("not implemented");
}

void todo()
{
    // TODO
}

void report()
{
    std::cout << "🚀 launch" << std::endl;
    std::cout << "a → b" << std::endl;
    std::cout << "x — y" << std::endl;
    std::cout << std::string(40, '=') << std::endl;
    std::cout << "==========" << std::endl;
}

void swallow()
{
    try {
        risky();
    } catch (...) {
    }
    try {
        risky();
    } catch (const std::exception &e) {
        std::cerr << e.what() << std::endl;
    }
}

#pragma warning(disable: 4996)
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

}

#define BUFFER_SIZE 512
#define GREETING "hello"
typedef unsigned long Counter;
typedef void (*Callback)(int);

void legacy(const char *src, std::vector<int> &items, Widget *w, va_list args)
{
    char buffer[BUFFER_SIZE];
    std::strcpy(buffer, src);
    vsprintf(buffer, src, args);
    int n = atoi(src);
    char *copy = (char *)malloc(n);
    free(copy);
    int *numbers = new int[n];
    delete[] numbers;
    double ratio = (double)n / 3;
    std::size_t width = (std::size_t)n;
    if (items.size() == 0 || w->items.size() > 0) {
        std::cout << GREETING << ratio << width << std::endl;
    }
    if (0 != items.size()) {
        w->child.reset(new Widget(n, src));
    }
    auto first = std::unique_ptr<Widget>(new Widget(n));
    std::shared_ptr<Widget> second(new Widget());
    if (w == NULL) {
        return;
    }
}

using namespace std;

void greet()
{
    cout << "hi" << endl;
}

struct Table {
    std::vector<int> rows;
    bool none() const { return rows.size() == 0; }
};

std::string plainFormat()
{
    return std::format("plain");
}

int checkout(int total)
{
    // you may want to tune the limit for your workload
    if (total < 0) {
        throw std::runtime_error("internal error");
    }
    EXPECT_TRUE(true);
    ASSERT_EQ(4, 4);
    return total;
}
