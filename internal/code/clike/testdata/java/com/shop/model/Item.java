package com.shop.model;

// A line item; components become fields.
public record Item(String name, double price) {
    public Item {
        if (price < 0) {
            throw new IllegalArgumentException("negative price: " + name);
        }
    }

    public Item discounted(double factor) {
        return new Item(name, price * factor);
    }
}
