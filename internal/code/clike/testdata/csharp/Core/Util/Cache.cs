namespace Shop
{
    namespace Core.Util
    {
        public interface IRepository<T> where T : Models.Entity
        {
            T? Find(int id);
        }

        public class Cache<T> : IRepository<T> where T : Models.Entity, new()
        {
            private readonly Dictionary<int, T> _items = new();
            private EventHandler? _evicted;

            public event EventHandler Evicted
            {
                add { _evicted += value; }
                remove { _evicted -= value; }
            }

            public T? Find(int id) => _items.TryGetValue(id, out var item) ? item : default;

            public bool TryFind(int id, out T? found)
            {
                found = this.Find(id);
                return found is not null;
            }

            public static implicit operator bool(Cache<T> cache) => cache._items.Count > 0;

            ~Cache()
            {
                _items.Clear();
            }
        }

        public class LoggingCache<T> : Cache<T> where T : Models.Entity, new()
        {
            public new T? Find(int @class) => base.Find(@class) ?? Fallback(nameof(Find));

            private static T? Fallback(string reason) => null;
        }
    }
}
