from dataclasses import dataclass
from enum import Enum


class Status(Enum):
    NEW = 1
    PAID = 2


@dataclass
class Item:
    """A line item."""

    name: str
    price: float = 0.0


class Base:
    def describe(self) -> str:
        return type(self).__name__


class Order(Base):
    TAX = 0.2

    def __init__(self, owner: str, items: list[Item] | None = None):
        self.owner = owner
        self.items = items or []
        self.status = Status.NEW

    @property
    def total(self) -> float:
        return sum(item.price for item in self.items) * (1 + self.TAX)

    def add(self, item: Item) -> "Order":
        self.items.append(item)
        self.last = item
        return self

    @classmethod
    def empty(cls, owner):
        return cls(owner)

    def describe(self) -> str:
        return super().describe() + self.owner


type Pair[T] = tuple[T, T]
