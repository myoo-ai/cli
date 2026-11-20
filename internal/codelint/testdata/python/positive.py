#!/usr/bin/env python3
"""
positive.py - examples of every rule.

Usage:
    python positive.py
"""
import os

# ===== Helpers =====


def get_user(user_id):
    """This is a function that gets the user."""
    # Step 1: Load the record from the environment
    record = os.environ.get(user_id)
    # Now we return the record to the caller
    return record


def count_items(items):
    counter = len(items)
    # Increment the counter variable here
    counter += 1
    return counter


def first_then():
    # First, read the whole file into memory
    data = open("x").read()
    # Then, split the text into separate words
    words = data.split()
    # Finally, show the words to the user
    print(words)


def placeholder():
    raise NotImplementedError


def todo_body():
    pass  # TODO


def chatty(x):
    # This explanation starts here
    # and keeps going
    # for a while
    # without saying much
    # about anything
    # in particular
    return x


def report():
    print("✅ Done")
    print("Next → step")
    print("one — two")
    print("=" * 60)
    print("==========")


def swallow():
    try:
        os.remove("x")
    except Exception:
        pass
    try:
        os.remove("y")
    except:
        print("failed")


def suppressed():
    value = os.environ["X"]  # type: ignore
    return value  # noqa


def lazy():
    import json
    return json.dumps({})


def nested(a):
    if a:
        for b in a:
            while b:
                try:
                    if b > 1:
                        return b
                except ValueError:
                    raise
    return None


def long_function():
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
    total += 70
    total += 71
    total += 72
    total += 73
    total += 74
    total += 75
    total += 76
    total += 77
    total += 78
    total += 79
    total += 80
    total += 81
    return total


def copy_one(rows, limit):
    result = []
    for row in rows:
        if row.size > limit and row.name.startswith("a"):
            result.append((row.name, row.size * 2, row.owner))
        elif row.size < 0:
            raise ValueError("negative size " + row.name)
    result.sort(key=lambda item: item[1])
    return result[:limit]


def copy_two(items, maximum):
    found = []
    for item in items:
        if item.weight > maximum and item.label.startswith("b"):
            found.append((item.label, item.weight * 3, item.parent))
        elif item.weight < 0:
            raise ValueError("negative weight " + item.label)
    found.sort(key=lambda entry: entry[1])
    return found[:maximum]


from os.path import *

CONFIG = {"mode": "fast"}
square = lambda x: x * x
cube: object = lambda value: value ** 3
zero = lambda: 0


def none_checks(a, b):
    if a == None or None != b:
        return a.size != None
    return b


def literal_identity(a, b):
    if a is "x" or b is not 1 or a is -1 or "y" is b:
        return a is [] or b is (1, 2) or a is {}
    return None


def defaults(a, items=[], opts={}, names=list(), seen=set(), *, extra: dict = {}):
    return a, items, opts, names, seen, extra


def type_checks(a, b):
    return type(a) == int or type(b) != Point or str == type(a)


def key_views(env, **kwargs):
    table = {}
    counts = dict(a=1)
    if "k" in table.keys() or "z" not in counts.keys() or "q" in CONFIG.keys():
        return [key for key in kwargs.keys()]
    for key in table.keys():
        print(key, env)
    return os.environ.keys() and "HOME" in os.environ.keys()


def index_loops(items, opts):
    for i in range(len(items)):
        print(items[i])
    for i in range(len(opts.names)): print(i, opts.names[i])


def sorting(items, words):
    low = sorted(items)[0]
    high = sorted(words, key=len)[-1]
    top = sorted(words, key=len, reverse=True)[0]
    bottom = sorted(items, reverse=True)[-1]
    return low, high, top, bottom, list(words)[0]


def prefixes(name, path):
    if name.startswith("x") or name.startswith("y") or name.startswith("z"):
        return 1
    return path.suffix.endswith(".py") or path.suffix.endswith(".pyi")


def handles(path):
    handle = open(path)
    config = json.load(open(path))
    return handle, config


def read_config(path):
    stream = open(path)
    text = stream.read()
    stream.close()
    return text


def format_without_fields(name):
    greeting = f"no fields here"
    return greeting + "plain".format() + "fixed" % ()


def charge(amount):
    # Fixed after code review: the amount used to be named total.
    # In production, you would call the payment provider here.
    if amount < 0:
        raise ValueError("Invalid value")
    assert True
    assert 1 == 1
    return "Hooray! Payment processed successfully!"


class ChargeTest(unittest.TestCase):
    def test_charge(self):
        self.assertTrue(True)
        self.assertEqual("a", "a")
