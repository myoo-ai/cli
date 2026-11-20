from .models import Order, Item as LineItem
from . import models
import shop.models as m

DISCOUNT = 0.1
counter = 0


def discount(order: Order, rate=DISCOUNT):
    global counter
    counter += 1
    total = order.total
    lines = [LineItem(name=i.name, price=i.price * (1 - rate)) for i in order.items]
    return total, lines


def best(orders):
    order = max(orders, key=lambda o: o.total)
    return order.owner, models.Status.PAID, m.Order.empty("x")


def shadow(x):
    def inner():
        nonlocal x
        x = x + 1
        return x

    x = [x for x in range(3) if (y := x) > 0]
    return inner(), y
