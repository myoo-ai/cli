import asyncio
from typing import overload


def log(fn):
    return fn


@log
async def fetch(url, /, *args, timeout: float = 1.0, **kw) -> bytes:
    async with asyncio.timeout(timeout) as t:
        pass
    try:
        data = await asyncio.sleep(0, b"")
    except (OSError, ValueError) as err:
        raise RuntimeError(url) from err
    with open(url) as fh, open(url) as other:
        data = fh.read() + other.read()
    return data


@overload
def parse(x: int) -> int: ...
@overload
def parse(x: str) -> str: ...
def parse(x):
    return x


class Registry:
    names = [n.upper() for n in ("a", "b")]
    handlers = {k: v for k, v in zip(names, names)}

    def register(self, *, name):
        self.names.append(name)
        return parse(x=name)


MAX_SIZE = 10
total = sum(i * MAX_SIZE for i in range(3))
