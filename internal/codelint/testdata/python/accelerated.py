"""Pure Python fallbacks replaced by a C accelerator when available."""
from _bisect_impl import *
from tokens import *

__all__ = tokens.__all__ + ["insort"]


def insort(items, item):
    items.append(item)
    items.sort()


try:
    from _fast_insort import *
except ImportError:
    pass

if sys.platform == "win32":
    from nt_helpers import *
