// Positive.cs - examples of every rule.
// Shows what code_lint reports.
using System;
using System.Diagnostics.CodeAnalysis;

namespace Fixture
{
    public class Positive
    {
        // ---------- Fields ----------
        private int counter;

        /// <summary>This is a method that gets the user.</summary>
        public string GetUser(string id)
        {
            // Step 1: Read it from the environment
            var user = Environment.GetEnvironmentVariable(id);
            // Here we return the user to the caller
            return user;
        }

        public int Count()
        {
            // Increment the counter variable here
            counter++;
            return counter;
        }

        public string Steps(string text)
        {
            // First, trim the whitespace off the text
            var trimmed = text.Trim();
            // Next, convert the text to upper case
            return trimmed.ToUpperInvariant();
        }

        public void Placeholder() => throw new NotImplementedException();

        public void Todo()
        {
            // TODO
        }

        public void Report()
        {
            Console.WriteLine("🔥 hot");
            Console.WriteLine("• item");
            Console.WriteLine("a — b");
            Console.WriteLine(new string('=', 40));
            Console.WriteLine("==========");
        }

        public void Swallow()
        {
            try
            {
                Risky();
            }
            catch
            {
            }
            try
            {
                Risky();
            }
            catch (Exception ex)
            {
                Console.WriteLine(ex);
            }
        }

#pragma warning disable CS0168
        [SuppressMessage("Style", "IDE0060")]
        public void Unused(int x)
        {
        }

        public int Chatty(int x)
        {
            // This explanation starts here
            // and keeps going
            // for a while
            // without saying much
            // about anything
            // in particular
            return x;
        }

        public int Nested(int[][] a)
        {
            foreach (var row in a)
            {
                foreach (var cell in row)
                {
                    if (cell > 0)
                    {
                        using (var d = Open())
                        {
                            if (cell == 2)
                            {
                                return cell;
                            }
                        }
                    }
                }
            }
            return 0;
        }

        public int LongFunction()
        {
            var total = 0;
            total += 0;
            total += 1;
            total += 2;
            total += 3;
            total += 4;
            total += 5;
            total += 6;
            total += 7;
            total += 8;
            total += 9;
            total += 10;
            total += 11;
            total += 12;
            total += 13;
            total += 14;
            total += 15;
            total += 16;
            total += 17;
            total += 18;
            total += 19;
            total += 20;
            total += 21;
            total += 22;
            total += 23;
            total += 24;
            total += 25;
            total += 26;
            total += 27;
            total += 28;
            total += 29;
            total += 30;
            total += 31;
            total += 32;
            total += 33;
            total += 34;
            total += 35;
            total += 36;
            total += 37;
            total += 38;
            total += 39;
            total += 40;
            total += 41;
            total += 42;
            total += 43;
            total += 44;
            total += 45;
            total += 46;
            total += 47;
            total += 48;
            total += 49;
            total += 50;
            total += 51;
            total += 52;
            total += 53;
            total += 54;
            total += 55;
            total += 56;
            total += 57;
            total += 58;
            total += 59;
            total += 60;
            total += 61;
            total += 62;
            total += 63;
            total += 64;
            total += 65;
            total += 66;
            total += 67;
            total += 68;
            total += 69;
            total += 70;
            total += 71;
            total += 72;
            total += 73;
            total += 74;
            total += 75;
            total += 76;
            total += 77;
            total += 78;
            total += 79;
            total += 80;
            total += 81;
            return total;
        }

        public List<string> CopyOne(List<Row> rows, int limit)
        {
            var result = new List<string>();
            foreach (var row in rows)
            {
                if (row.Size > limit && row.Name.StartsWith("a"))
                {
                    result.Add(row.Name + ":" + row.Size * 2 + ":" + row.Owner);
                }
                else if (row.Size < 0)
                {
                    throw new ArgumentException("negative size " + row.Name);
                }
            }
            return result.GetRange(0, limit);
        }

        public List<string> CopyTwo(List<Item> items, int maximum)
        {
            var found = new List<string>();
            foreach (var item in items)
            {
                if (item.Weight > maximum && item.Label.StartsWith("b"))
                {
                    found.Add(item.Label + ":" + item.Weight * 3 + ":" + item.Parent);
                }
                else if (item.Weight < 0)
                {
                    throw new ArgumentException("negative weight " + item.Label);
                }
            }
            return found.GetRange(0, maximum);
        }
    }
}

namespace Fixture.Rules
{
    public class LanguageRules
    {
        public bool HasItems(List<int> items, List<int> others)
        {
            var some = items.Count() > 0;
            var none = 0 == others.Where(x => x > 2).Count();
            return some && none && items.Count() < 1;
        }

        public int FirstLarge(List<int> items)
        {
            if (items.Where(x => x > 10).Any())
            {
                return items.Where(x => x > 10).FirstOrDefault();
            }
            return 0;
        }

        public bool IsBlank(string name, string title)
        {
            if (name == "" || string.Empty != title)
            {
                return true;
            }
            return title.Equals("");
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
                throw error;
            }
        }

        public async Task<int> LoadAsync(Task<int> pending)
        {
            var first = pending.Result;
            FetchAsync(first).Wait();
            var second = FetchAsync(first).GetAwaiter().GetResult();
            return first + second + await pending;
        }

        public string Join(List<string> parts)
        {
            string joined = "";
            foreach (var part in parts)
            {
                joined += part;
                joined = joined + ",";
            }
            return joined;
        }

        public async void Refresh()
        {
            await FetchAsync(1);
        }

        public void Draw(object shape)
        {
            if (shape is Circle && shape != null)
            {
                ((Circle)shape).Draw();
            }
            if (shape is Square)
            {
                var square = shape as Square;
                square.Draw();
            }
        }

        public void Rename(string name, int length)
        {
            if (name == null)
            {
                throw new ArgumentNullException("name");
            }
            if (length < 0)
            {
                throw new ArgumentException("must not be negative", "length");
            }
        }
    }
}

namespace Fixture.Rules
{
    public class BlockPredicate
    {
        public Item Find(List<Item> items, string name)
        {
            return items.Where(p =>
            {
                return p.Label == name;
            }).SingleOrDefault();
        }
    }
}

public class FormatNoPlaceholders
{
    public string Render() => $"plain" + string.Format("fixed");
}

public class Checkout
{
    // Per your feedback, totals are rounded once.
    public string Pay(int total)
    {
        if (total < 0) throw new InvalidOperationException("Unexpected error");
        Assert.True(true);
        Assert.AreEqual(3, 3);
        return "Awesome! Paid.";
    }
}
