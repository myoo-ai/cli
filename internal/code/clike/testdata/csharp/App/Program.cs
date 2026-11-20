using Shop.Core.Models;
using Calc = Shop.Core.Pricing.Calculator;
using static System.Math;

namespace Shop.App;

public delegate void Logger(string message);

internal sealed class Program(Logger log)
{
    private int _runs;

    public static void Main(string[] args)
    {
        var program = new Program(Console.WriteLine);
        program.Run(args.Length > 0 ? args[0] : "guest");
    }

    public void Run(string customer)
    {
        var order = new Order(customer) { Customer = customer };
        order.Add("pen", 1.5m);
        order += new Line("ink", 3m);
        decimal discounted = order.Discount(0.1m);
        log($"{order.Customer} pays {Round(discounted, 2):F2}");

        int Twice(int x) => x * 2;
        _runs = Twice(_runs);

        if (order is { Count: > 1 } big && big.Total > 2)
        {
            Describe(big);
        }

        foreach (var (sku, price) in Pairs(order))
        {
            log(sku + price);
        }

#if DEBUG
        string raw = """
            {"raw": "string with } and { braces"}
            """;
#endif
        var nested = $$"""{"total": {{order.Total}}}""";
        var verbatim = @"C:\path\""quoted""";
        log(raw + nested + verbatim);
        Func<int, int> square = n => n * n;
        log(Helper(square(3)));

        string Helper(int value) => Calc.Sum(Array.Empty<Line>()) + value.ToString();
    }

    private void Guard(Order order)
    {
        try
        {
            using var writer = new System.IO.StringWriter();
            lock (order)
            {
                order.Note("guarded");
            }
        }
        catch (InvalidOperationException ex) when (ex.Message.Length > 0)
        {
            log(ex.Message);
        }
        finally
        {
            _runs = 0;
        }
    }

    private static IEnumerable<(string, decimal)> Pairs(Order order)
    {
        yield return ("total", order.Total);
    }

    private string Describe(Order order) => order switch
    {
        { Count: 0 } => "empty",
        Order o when o.Total > 100 => "big",
        _ => "normal",
    };
}
