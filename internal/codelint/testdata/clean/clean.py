import os


def lookup(name):
    """Return the environment value for name, or the empty string."""
    return os.environ.get(name, "")
