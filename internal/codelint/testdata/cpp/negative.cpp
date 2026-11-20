// Copyright 2026 Example Corp.
#pragma once
#include <stdexcept>
#include <string>

namespace demo {

class Shape {
public:
    virtual double area() { throw std::logic_error("not implemented"); }
};

/// Parses n from text, or returns fallback when text is not a number.
int parseOr(const std::string &text, int fallback)
{
    try {
        return std::stoi(text);
    } catch (const std::invalid_argument &) {
        return fallback;
    }
}

void cleanup()
{
    try {
        removeTemp();
    } catch (...) {
        // temp files are removed by the OS on reboot anyway
    }
}

}

#define VERSION_SUFFIX "-beta"
#define FEATURE_LEVEL 2
#if FEATURE_LEVEL > 1
#define SQUARE(x) ((x) * (x))
#endif
#define HEADER_GUARD_LIKE

struct Buffer {
    Buffer(const Buffer &) = delete;
    Buffer &operator=(const Buffer &) = delete;
    static void operator delete(void *p);
    bool empty() const { return items.size() == 0; }
    std::vector<int> items;
};

int modern(std::vector<int> &items, std::unique_ptr<Widget> &w, const char *text, Pool *pool)
{
    const char *label = "v" VERSION_SUFFIX;
    (void)label;
    auto n = static_cast<int>(items.size());
    std::size_t bytes = sizeof(int) * n;
    if (items.empty() || items.size() > 2 || n + items.size() == 0) {
        w = std::make_unique<Widget>(n);
    }
    std::unique_ptr<Widget, Deleter> custom(new Widget(n), Deleter{});
    std::unique_ptr<Widget> plain(new Widget);
    std::cout << text << '\n' << std::flush;
    w->free();
    pool->malloc(bytes);
    auto twice = [](int value) { return value * 2; };
    std::function<void(int *)> callback;
    int (*parse)(char *) = nullptr;
    void declared(char *);
    return std::stoi(text) + twice(SQUARE(n));
}

#if defined(__clang__)
#define USE_CLANG 1
#else
#define USE_CLANG 0
#endif

struct Counter {
    Counter operator++(int) &;
    Counter operator--(int) &;
};

std::string vsprintf(string_view fmt, format_args args);

std::string formatted(string_view fmt, format_args args, string_view text, const std::vector<int> &items)
{
    if (USE_CLANG && text.size() == 0 && items[0] > 0) {
        return vsprintf(fmt, args);
    }
    return std::string(text.data(), text.size());
}

extern "C" {
char *api_copy(const char *text)
{
    char *out = static_cast<char *>(std::malloc(std::strlen(text) + 1));
    return out;
}
}

std::string countFormat(int n)
{
    return std::format("{} items", n);
}
