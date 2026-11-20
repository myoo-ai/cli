using System.Linq;
using Shop.Core.Pricing;

namespace Shop.Core.Models;

/// <summary>An order with lines.</summary>
[Serializable]
public partial class Order : Entity, IComparable<Order>
{
    private readonly List<Line> _lines = new();
    public const int MaxLines = 50;

    public string Customer { get; init; } = "";

    public decimal Total => Calculator.Sum(_lines);

    public int Count
    {
        get { return _lines.Count; }
        set
        {
            if (value > MaxLines) throw new ArgumentException($"too many: {value}");
        }
    }

    public event EventHandler? Changed;

    public Order(string customer) : base(1)
    {
        Customer = customer;
    }

    public void Add(Line line) => _lines.Add(line);

    public void Add(string sku, decimal price)
    {
        Add(new Line(sku, price));
        Changed?.Invoke(this, EventArgs.Empty);
    }

    public int CompareTo(Order? other) => Total.CompareTo(other?.Total ?? 0);

    public static Order operator +(Order a, Line b)
    {
        a.Add(b);
        return a;
    }

    public Line this[int index] => _lines[index];
}
