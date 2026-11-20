#pragma once
#include <string>

namespace demo {
using namespace std::string_literals;
inline std::string greet()
{
    using namespace std;
    return string("hi");
}
}
