using Shop.Core.Models;

var orders = new List<Order> { new("ann"), new Order("bob") };
foreach (var o in orders)
{
    Print(o);
}
var summary = new Summary(orders.Count);

static void Print(Order order) => Console.WriteLine(order.Customer);

record Summary(int Count);
