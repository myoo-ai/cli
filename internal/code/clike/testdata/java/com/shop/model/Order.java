package com.shop.model;

import java.util.ArrayList;
import java.util.List;

/** An order with line items. */
public class Order extends Entity implements Comparable<Order> {
    private final List<Item> items = new ArrayList<>();
    private String customer;
    static int created = 0, cancelled;

    public Order(String customer) {
        this.customer = customer;
        created++;
    }

    public Order() {
        this("anonymous");
    }

    public void add(Item item) {
        items.add(item);
    }

    public void add(String name, double price) {
        add(new Item(name, price));
    }

    public double total() {
        double sum = 0;
        for (Item item : items) {
            sum += item.price();
        }
        return sum;
    }

    @Override
    public int compareTo(Order other) {
        return Double.compare(total(), other.total());
    }

    public static class Builder {
        private final Order order = new Order();

        public Builder item(String name, double price) {
            order.add(name, price);
            return this;
        }

        public Order build() {
            return order;
        }
    }
}
