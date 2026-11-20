namespace Shop.Core.Models;

public abstract record Entity(int Id)
{
    public override string ToString() => $"#{Id}";
}

public record Line(string Sku, decimal Price);

public readonly record struct Money(decimal Amount, string Currency);
