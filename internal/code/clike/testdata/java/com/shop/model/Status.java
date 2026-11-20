package com.shop.model;

public enum Status {
    OPEN("open"),
    PAID("paid") {
        @Override
        public boolean isFinal() {
            return true;
        }
    },
    CANCELLED("cancelled");

    private final String label;

    Status(String label) {
        this.label = label;
    }

    public boolean isFinal() {
        return false;
    }

    public String label() {
        return label;
    }
}
