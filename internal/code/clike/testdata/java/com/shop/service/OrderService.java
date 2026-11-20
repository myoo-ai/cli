package com.shop.service;

import com.shop.model.*;
import com.shop.model.Order.Builder;
import static com.shop.model.Status.PAID;
import java.util.Comparator;
import java.util.function.Function;

public class OrderService implements Notifier {
    private final Repository<Order> repository;

    public OrderService(Repository<Order> repository) {
        this.repository = repository;
    }

    public Order checkout(String customer) {
        Order order = new Builder().item("pen", 1.5).item("ink", 3).build();
        var copy = new Order(customer);
        copy.add("box", 2);
        repository.save(order);
        notify(order.total());
        return order;
    }

    public String describe(Object value) {
        if (value instanceof Order o && o.total() > 10) {
            return "big order " + o.getId();
        }
        return switch (value) {
            case Item(String name, double price) when price > 5 -> name;
            case Status s -> s.label();
            default -> "other";
        };
    }

    public boolean settled(Status status) {
        switch (status) {
            case PAID:
                return true;
            case CANCELLED:
            default:
                return status == PAID;
        }
    }

    public Comparator<Order> byTotal() {
        Function<Order, Double> key = o -> o.total();
        return (a, b) -> Double.compare(key.apply(a), b.total());
    }

    public Runnable task(int count) {
        int total = count;
        return new Runnable() {
            private int runs;

            @Override
            public void run() {
                runs++;
                log(total + runs);
            }

            private void log(int n) {
                notify(n);
            }
        };
    }

    @Override
    public void notify(double amount) {
        String text = """
            Total: "%s"
            """.formatted(amount);
        System.out.println(text);
    }
}
