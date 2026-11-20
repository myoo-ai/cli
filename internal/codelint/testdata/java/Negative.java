// Copyright 2026 Example Corp.
package fixture;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

/**
 * Reads configuration files, caching parsed results per path.
 */
public class Negative {
    /**
     * Returns the cached configuration for path, parsing the file on a miss.
     */
    public String load(Path path) throws IOException {
        return Files.readString(path);
    }

    public void narrow(Path p) {
        try {
            Files.delete(p);
        } catch (IOException e) {
        }
        try {
            Files.delete(p);
        } catch (Exception e) {
            // deleting is best effort, the OS cleans the temp dir later
        }
    }

    @SuppressWarnings("unchecked") // the list comes from our own typed cache
    public List<String> cast(Object o) {
        return (List<String>) o;
    }
}

interface Shape {
    default double area() {
        throw new UnsupportedOperationException("not implemented");
    }
}

abstract class Base {
    void run() {
        throw new UnsupportedOperationException("not implemented");
    }
}

class ModernIdioms {
    private final java.util.ArrayList<String> names = new java.util.ArrayList<>();
    private final int[] counts = new int[4];

    boolean isEmpty() {
        return names.size() == 0;
    }

    boolean checks(java.util.List<String> list, String text, String other) {
        String label = "Vector and StringBuffer";
        int n = list.size() + 0;
        return list.isEmpty() || counts.length == 0 || list.size() == 10 || text.length() > 2
            || text.equals("admin") || text == other || text.charAt(0) == 'a';
    }

    void report(Exception e, java.io.PrintStream out) {
        e.printStackTrace(out);
    }

    Object convert(byte[] bytes, char[] chars) {
        Integer n = Integer.valueOf(4);
        String a = new String(bytes, java.nio.charset.StandardCharsets.UTF_8);
        return new String(chars) + n + a;
    }

    String read(java.io.File file) throws java.io.IOException {
        try (java.io.FileReader reader = new java.io.FileReader(file)) {
            return "" + (char) reader.read();
        }
    }

    void release(java.io.Closeable shared) throws java.io.IOException {
        try {
            shared.flush();
        } finally {
            shared.close();
        }
    }

    void handOff(java.io.File file, java.util.function.Consumer<java.io.Reader> sink) throws java.io.IOException {
        java.io.FileReader reader = new java.io.FileReader(file);
        boolean handed = false;
        try {
            sink.accept(reader);
            handed = true;
        } finally {
            if (!handed) reader.close();
        }
    }

    String clean(String path, String value) {
        return path.replaceAll("\\s+", "_").replaceAll("a.b", "c").replace("/", value);
    }

    String join(java.util.List<String> parts) {
        StringBuilder result = new StringBuilder();
        int count = 0;
        for (String part : parts) {
            String line = "";
            line += part;
            count += 1;
            result.append(line);
        }
        return result.toString() + count;
    }

    int total(java.util.Map<String, Integer> counts, java.util.Map<String, Integer> other) {
        int sum = 0;
        for (java.util.Map.Entry<String, Integer> e : counts.entrySet()) {
            sum += e.getValue();
        }
        for (String key : counts.keySet()) {
            sum += other.get(key);
        }
        return sum;
    }

    int width(Object shape) {
        if (shape instanceof Rect r) {
            return r.width;
        }
        if (shape instanceof Rect || shape == null) {
            return 1;
        }
        return 0;
    }

    java.util.List<String> none() {
        return Collections.emptyList();
    }
}

class CorpusLookAlikes {
    private final Attributes attributes = new Attributes();

    boolean shapes(StringBuilder accum, CharSequence cs, Object value) {
        return accum.length() == 0 || cs.length() > 0 || attributes.size() > 0 || value instanceof StringBuffer;
    }

    StringBuffer toBuffer(String text) {
        return new StringBuffer(text);
    }

    void append(StringBuffer buffer, Appender appender) {
        appender.appendTo(new StringBuffer(), buffer);
    }
}

class SharedBuffer {
    protected StringBuffer buffer;

    SharedBuffer(StringBuffer given) {
        this.buffer = given != null ? given : new StringBuffer(512);
    }
}

class FormatPlaceholders {
    String render(String name) {
        System.out.printf("%s%n", name);
        return String.format(Locale.ROOT, "%d", 1) + MessageFormat.format("hi {0}", name) + "%s".formatted(name);
    }
}

class Checkout {
    // The cart keeps the totals that held until now.
    String pay(int total) {
        if (total < 0) {
            throw new IllegalArgumentException("total must not be negative: " + total);
        }
        assertEquals(total, 1);
        assertThat(total).isEqualTo(5);
        return "Paid " + total;
    }
}
