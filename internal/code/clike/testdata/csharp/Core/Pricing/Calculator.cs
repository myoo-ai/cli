using Shop.Core.Models;

namespace Shop.Core.Pricing
{
    public static class Calculator
    {
        public static decimal Sum(IEnumerable<Line> lines)
        {
            decimal total = 0;
            foreach (var line in lines)
            {
                total += line.Price;
            }
            return total;
        }

        public static decimal Discount(this Order order, decimal rate) => order.Total * (1 - rate);

        public static IEnumerable<string> Expensive(IEnumerable<Line> lines, decimal min) =>
            from l in lines
            where l.Price > min
            orderby l.Price descending
            select l.Sku;
    }
}
