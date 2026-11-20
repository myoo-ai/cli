// <copyright file="Negative.cs" company="Example">Copyright 2026 Example Corp.</copyright>
using System;
using System.Diagnostics.CodeAnalysis;
using System.IO;

namespace Fixture
{
    public abstract class Shape
    {
        public virtual double Area() => throw new NotImplementedException();
    }

    public class Negative
    {
        /// <summary>Deletes the file at path. A missing file is not an error.</summary>
        public void Delete(string path)
        {
            try
            {
                File.Delete(path);
            }
            catch (IOException)
            {
            }
            try
            {
                File.Delete(path);
            }
            catch (Exception ex) when (ex is UnauthorizedAccessException)
            {
            }
        }

#pragma warning disable CS0618 // the replacement API needs .NET 9, we target .NET 8
        [SuppressMessage("Style", "IDE0060", Justification = "Kept for binary compatibility")]
        public void Legacy(int unused)
        {
        }
#pragma warning restore CS0618
    }
}

namespace Fixture.Rules
{
    public class LanguageRules
    {
        public bool HasItems(List<int> items, int[] values)
        {
            var some = items.Count > 0 || values.Length != 0;
            var many = items.Count() > 1 || items.Count(x => x > 2) == 0;
            return some && many && 1 + items.Count() > 0 && items.Any();
        }

        public int FirstLarge(List<int> items)
        {
            var indexed = items.Where((x, i) => x > i).First();
            var projected = items.Where(x => x > 10).Select(x => x * 2).First();
            return indexed + projected + items.First(x => x > 10);
        }

        public bool IsBlank(string name, string title)
        {
            return string.IsNullOrEmpty(name) || title.Length == 0 || name + title == "" || title.Equals("a");
        }

        public void Save(string path)
        {
            try
            {
                System.IO.File.Delete(path);
            }
            catch (System.IO.IOException error)
            {
                Log(error);
                throw;
            }
            try
            {
                System.IO.File.Delete(path);
            }
            catch (System.IO.IOException error)
            {
                Action retry = () => { throw error; };
                throw new InvalidOperationException("delete failed", error);
            }
        }

        public async Task<int> LoadAsync(Task<int> pending, Response response, SemaphoreSlim gate)
        {
            var first = await pending;
            var value = response.Result;
            gate.Wait();
            var later = Task.Run(() => pending.Result);
            return first + value + await later;
        }

        public int Load(Task<int> pending)
        {
            return pending.Result;
        }

        public string Join(List<string> parts)
        {
            var builder = new StringBuilder();
            foreach (var part in parts)
            {
                string line = "";
                line += part;
                builder.Append(line);
            }
            return builder.ToString();
        }

        public async Task RefreshAsync()
        {
            await FetchAsync(1);
        }

        public async void Clicked(object sender, RoutedEventArgs e)
        {
            await FetchAsync(1);
        }

        public async void OnLoaded()
        {
            await FetchAsync(2);
        }

        public async void Tick()
        {
            await FetchAsync(3);
        }

        public void Subscribe(Timer timer)
        {
            timer.Elapsed += Tick;
        }

        public void Draw(object shape)
        {
            if (shape is Circle circle)
            {
                circle.Draw();
            }
            if (shape is Square)
            {
                Count((Square)other);
            }
            if (!(shape is Line))
            {
                ((Line)shape).Draw();
            }
        }

        public bool Retry(List<int> items, Response response)
        {
            try
            {
                response.Result = items?.Count() > 0;
            }
            catch (TimeoutException error)
            {
                try
                {
                    Reset();
                }
                catch (InvalidOperationException)
                {
                    throw error;
                }
            }
            return response.Result;
        }

        public void Rename(string name, int length)
        {
            if (name == null)
            {
                throw new ArgumentNullException(nameof(name));
            }
            if (length < 0)
            {
                throw new ArgumentException("length", nameof(length));
            }
            throw new ArgumentOutOfRangeException("other");
        }
    }
}

public class FormatPlaceholders
{
    public string Render(string name) => $"hi {name}" + $$"""{{name}}""" + string.Format("{0}", name) + @"C:\plain";
}

public class Checkout
{
    public string Pay(int total)
    {
        if (total < 0) throw new InvalidOperationException($"total must not be negative: {total}");
        Assert.True(total > 0);
        Assert.AreEqual(3, total);
        return "Paid";
    }
}
