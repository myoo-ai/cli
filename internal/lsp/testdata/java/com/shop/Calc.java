package com.shop;

/** Accumulates a total. */
public class Calc {
    private int total;

    /** Adds {@code a} and {@code b} to the total. */
    public int add(int a, int b) {
        total += a + b;
        return total;
    }

    public int add(int a) {
        return add(a, 0);
    }
}
