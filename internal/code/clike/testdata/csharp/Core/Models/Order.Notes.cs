namespace Shop.Core.Models
{
    public partial class Order
    {
        private readonly List<string> _notes = new List<string>();

        public void Note(string text)
        {
            _notes.Add($"{Customer}: {text,10:G}");
            Changed?.Invoke(this, EventArgs.Empty);
        }
    }
}
