/*
 * Positive.java - examples.
 * Shows every rule.
 */
package fixture;

import java.util.List;

public class Positive {
    // ===== Fields =====
    private int counter;

    /** This is a method that gets the user. */
    public String getUser(String id) {
        // Step 1: Look the user up in the environment
        String user = System.getenv(id);
        // Now we return the user to the caller
        return user;
    }

    public int count() {
        // Increment the counter variable here
        counter++;
        return counter;
    }

    public void steps(List<String> parts) {
        // 1. Sort the parts in natural order
        parts.sort(null);
        // 2. Print all of the parts to stdout
        System.out.println(parts);
    }

    public void placeholder() {
        throw new UnsupportedOperationException("Not implemented yet");
    }

    public void todo() {
        // TODO: implement
    }

    public void report() {
        System.out.println("🎉 done");
        System.out.println("ok ✔");
        System.out.println("a — b");
        System.out.println("=".repeat(50));
        System.out.println("==========");
    }

    public void swallow() {
        try {
            risky();
        } catch (Exception e) {
        }
        try {
            risky();
        } catch (Throwable t) {
            t.printStackTrace();
        }
    }

    @SuppressWarnings("unchecked")
    public List<String> raw(Object o) {
        return (List<String>) o;
    }

    public int chatty(int x) {
        // This explanation starts here
        // and keeps going
        // for a while
        // without saying much
        // about anything
        // in particular
        return x;
    }

    public int nested(int[][] a) {
        for (int[] row : a) {
            for (int cell : row) {
                if (cell > 0) {
                    while (cell > 1) {
                        if (cell == 2) {
                            return cell;
                        }
                        cell--;
                    }
                }
            }
        }
        return 0;
    }

    public int longFunction() {
        int total = 0;
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

    public List<String> copyOne(List<Row> rows, int limit) {
        List<String> result = new ArrayList<>();
        for (Row row : rows) {
            if (row.size > limit && row.name.startsWith("a")) {
                result.add(row.name + ":" + row.size * 2 + ":" + row.owner);
            } else if (row.size < 0) {
                throw new IllegalArgumentException("negative size " + row.name);
            }
        }
        return result.subList(0, limit);
    }

    public List<String> copyTwo(List<Item> items, int maximum) {
        List<String> found = new ArrayList<>();
        for (Item item : items) {
            if (item.weight > maximum && item.label.startsWith("b")) {
                found.add(item.label + ":" + item.weight * 3 + ":" + item.parent);
            } else if (item.weight < 0) {
                throw new IllegalArgumentException("negative weight " + item.label);
            }
        }
        return found.subList(0, maximum);
    }
}

class LegacyIdioms {
    private final Vector<String> names = new Vector<>();

    boolean noNames(java.util.List<String> list, String text) {
        if (list.size() == 0) {
            return true;
        }
        return 0 < text.length() && list.size() > 0;
    }

    boolean isAdmin(String role) {
        return role == "admin" || "root" != role;
    }

    void report(Exception e) {
        e.printStackTrace();
    }

    Integer box(int n) {
        Boolean flag = new Boolean(true);
        String copy = new String("copy");
        return new Integer(n);
    }

    String read(java.io.File file) throws java.io.IOException {
        java.io.FileReader reader = null;
        try {
            reader = new java.io.FileReader(file);
            return "" + (char) reader.read();
        } finally {
            if (reader != null) {
                reader.close();
            }
        }
    }

    String clean(String path) {
        return path.replaceAll("/", "_");
    }

    String join(java.util.List<String> parts) {
        String result = "";
        for (String part : parts) {
            result += part;
        }
        return result;
    }

    int total(java.util.Map<String, Integer> counts) {
        int sum = 0;
        for (String key : counts.keySet()) {
            sum += counts.get(key);
        }
        return sum;
    }

    int width(Object shape) {
        if (shape instanceof Rect) {
            return ((Rect) shape).width;
        }
        return 0;
    }

    java.util.List<String> none() {
        return Collections.EMPTY_LIST;
    }
}

class LegacyLocals {
    String render(int n) {
        StringBuffer out = new StringBuffer();
        out.append(n);
        return out.toString();
    }
}

class FormatNoPlaceholders {
    String render() {
        System.out.printf("ready");
        return String.format("plain") + "fixed".formatted();
    }
}

class Checkout {
    // Note for reviewers: the total is rounded twice.
    String pay(int total) {
        if (total < 0) {
            throw new IllegalArgumentException("Invalid argument");
        }
        assertTrue(true);
        Assertions.assertEquals("x", "x");
        assertThat(5).isEqualTo(5);
        return "Success! Payment received.";
    }
}
