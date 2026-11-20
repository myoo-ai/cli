from shop import Order, Item, pricing
from shop.models import *

order = Order("ana").add(Item("tea", 2.5))
order.add(Item(name="cake", price=3))
print(pricing.discount(order, rate=0.2), order.status, Status.PAID)
for i, line in enumerate(order.items):
    print(f"{i}: {line.name!r:>{i}} {order.owner=}")

match order.status:
    case Status.NEW:
        pass
    case Item(name=n) | Order(owner=n):
        print(n)
    case _:
        pass
