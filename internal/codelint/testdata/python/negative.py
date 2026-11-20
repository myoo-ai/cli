# Copyright 2026 Example Corp. Licensed under the MIT license.
"""Accepted patterns."""
from __future__ import annotations

import abc
import logging
import os

log = logging.getLogger(__name__)


class Shape(abc.ABC):
    @abc.abstractmethod
    def area(self):
        raise NotImplementedError


class Exporter:
    def export(self):
        raise NotImplementedError("subclasses write the file")


class CsvExporter(Exporter):
    def export(self):
        return "a,b"


def parse_mode(mode):
    if mode == "fast":
        return 1
    raise NotImplementedError(f"mode {mode}")


def lookup(user_id):
    """Look the id up in the environment, falling back to the guest account."""
    # Environment lookups are cheap, so nothing is cached.
    return os.environ.get(user_id, "guest")


def numbered_list():
    # The parser accepts:
    # 1. a name
    # 2. a value
    return None


def single_numbered():
    # 1. only one numbered comment is not a sequence
    return 1


def swallow_narrow():
    try:
        os.remove("x")
    except FileNotFoundError:
        pass
    try:
        os.remove("y")
    except Exception:
        # the file may be locked by another process, retry on next run
        pass
    try:
        os.remove("z")
    except Exception as exc:
        log.error("remove failed: %s", exc)
        raise


def suppressed():
    return os.environ["X"]  # type: ignore[index]  # stubs lack this key


def optional_dependency():
    try:
        import yaml
    except ImportError:
        yaml = None
    return yaml


def deferred():
    import json  # deferred to keep startup fast
    return json


def separators():
    print("-" * 3)
    text = "=" * 60
    print("a - b")
    return text


def counter_comment(counter):
    # avoid overflow on 32-bit platforms
    counter += 1
    return counter


def nested_ok(a):
    if a:
        for b in a:
            while b:
                if b > 1:
                    return b
    return None


def short_function():
    total = 0
    total += 0
    total += 1
    total += 2
    total += 3
    total += 4
    total += 5
    total += 6
    total += 7
    total += 8
    total += 9
    total += 10
    total += 11
    total += 12
    total += 13
    total += 14
    total += 15
    total += 16
    total += 17
    total += 18
    total += 19
    total += 20
    total += 21
    total += 22
    total += 23
    total += 24
    total += 25
    total += 26
    total += 27
    total += 28
    total += 29
    total += 30
    total += 31
    total += 32
    total += 33
    total += 34
    total += 35
    total += 36
    total += 37
    total += 38
    total += 39
    total += 40
    total += 41
    total += 42
    total += 43
    total += 44
    total += 45
    total += 46
    total += 47
    total += 48
    total += 49
    total += 50
    total += 51
    total += 52
    total += 53
    total += 54
    total += 55
    total += 56
    total += 57
    total += 58
    total += 59
    total += 60
    total += 61
    total += 62
    total += 63
    total += 64
    total += 65
    total += 66
    total += 67
    total += 68
    total += 69
    return total


TABLE = [
    {"name": "row0", "size": 0, "owner": "x"},
    {"name": "row1", "size": 1, "owner": "x"},
    {"name": "row2", "size": 2, "owner": "x"},
    {"name": "row3", "size": 3, "owner": "x"},
    {"name": "row4", "size": 4, "owner": "x"},
    {"name": "row5", "size": 5, "owner": "x"},
    {"name": "row6", "size": 6, "owner": "x"},
    {"name": "row7", "size": 7, "owner": "x"},
    {"name": "row8", "size": 8, "owner": "x"},
    {"name": "row9", "size": 9, "owner": "x"},
    {"name": "row10", "size": 10, "owner": "x"},
    {"name": "row11", "size": 11, "owner": "x"},
    {"name": "row12", "size": 12, "owner": "x"},
    {"name": "row13", "size": 13, "owner": "x"},
    {"name": "row14", "size": 14, "owner": "x"},
    {"name": "row15", "size": 15, "owner": "x"},
    {"name": "row16", "size": 16, "owner": "x"},
    {"name": "row17", "size": 17, "owner": "x"},
    {"name": "row18", "size": 18, "owner": "x"},
    {"name": "row19", "size": 19, "owner": "x"},
    {"name": "row20", "size": 20, "owner": "x"},
    {"name": "row21", "size": 21, "owner": "x"},
    {"name": "row22", "size": 22, "owner": "x"},
    {"name": "row23", "size": 23, "owner": "x"},
    {"name": "row24", "size": 24, "owner": "x"},
    {"name": "row25", "size": 25, "owner": "x"},
    {"name": "row26", "size": 26, "owner": "x"},
    {"name": "row27", "size": 27, "owner": "x"},
    {"name": "row28", "size": 28, "owner": "x"},
    {"name": "row29", "size": 29, "owner": "x"},
]


class Model:
    handler: object = lambda self: None

    def __enter__(self):
        self.stream = open("model.txt")
        return self


def compare_none(a, b, query, frame):
    if a is None or b is not None or a == b or a is b:
        return query.filter(Model.handler == None)
    return frame["a"] == None


def compare_values(a, b, flag):
    return a is flag or a is not (b) or a == "x" or (4, 5) == b


def safe_defaults(a, _cache={}, items=None, rows=(), name="x", count=0, check=lambda x=[]: x):
    return a, _cache, items, rows, name, count, check


def compare_types(a, b, get_class):
    return type(a) == type(b) or type(a) is int or isinstance(a, str) or type(a) == get_class()


def other_keys(row, store, mapping, values):
    if "k" in row.keys() or "z" in store.keys("p") or 1 in values.values():
        return [key for key in mapping.keys()]
    return "k" in mapping.keys() | {"x"}


def index_writes(items):
    for i in range(len(items)):
        items[i] = items[i] + 1
    for i in range(len(items)):
        items[i], items[0] = items[0], items[i]
    for i in range(len(items)):
        print(i)
    for i in range(1, len(items)):
        print(items[i])
    return [items[i] for i in range(len(items))]


def lambda_values(holder):
    first, second = lambda x: x, 10
    holder.callback = lambda x: x
    return first, second


def partial_sorting(items, flag):
    second = sorted(items)[1]
    unknown = sorted(items, reverse=flag)[0]
    head = sorted(items)[0:2]
    return second, unknown, head, list(items)[1], list(*items)[0], list(items)[-1]


def mixed_prefixes(a, b, prefix, other):
    if a.startswith("x") or b.startswith("y"):
        return 1
    if a.startswith("x") or a.endswith("y") or a.startswith(prefix) or a.startswith("z"):
        return 2
    if not a.startswith("x") or not a.startswith("y"):
        return 3
    return other and a.startswith("x") or a.startswith("y") and other


def managed_files(stack, holder):
    with open("p") as fh, open("q") as gh:
        print(fh, gh)
    descriptor = os.open("p", os.O_RDONLY)
    kept = stack.enter_context(open("r"))
    reused = open("s")
    with reused:
        print(reused)
    holder.out = open("t", "w")
    print(descriptor, kept)
    return open("u")


def stream_lines():
    yield open("v")


def escaping_handles(path, use_stdin, readers):
    if use_stdin:
        stream = sys.stdin
    else:
        stream = open(path)
    proxy = open(path, "rb")
    readers.append(open(path))
    return stream, proxy, (path, open(path))


def wrapped_handle(path):
    return Wrapper(open(path))


def closed_by_helper(path):
    fp = open(path, "w+b")
    with contextlib.closing(fp) as fp:
        return fp.name


def memo(key, cache={}, calls=[0]):
    calls[0] += 1
    if key not in cache:
        cache[key] = key * 2
    return cache[key]


def fast_key(args, kinds={int, str}, len=len, type=type):
    return len(args) == 1 and type(args[0]) in kinds


def shrinking(words, items):
    for i in range(len(words)):
        if words[i][0] == "#":
            del words[i:]
            break
    for i in range(len(items)):
        if items[i]:
            items.append(i)


def format_with_fields(name):
    message = (f"hello {name}, "
               f"welcome")
    return message + "{}!".format(name) + "%d%%" % 5 + f"{{x}} {name}"


def charge(amount, case):
    """Charge amount. A naive implementation would be quadratic, so the totals
    are cached. The key is formerly known as merchant_id in the API."""
    if amount < 0:
        raise ValueError(f"amount must be positive, got {amount}")
    if not amount:
        raise ValueError("Invalid value", amount)
    assert amount == 1
    case.assertTrue(amount)
    case.assertEqual(amount, 1)
    return {"placeholder": "Jane Doe", "message": "Charged %d" % amount}
