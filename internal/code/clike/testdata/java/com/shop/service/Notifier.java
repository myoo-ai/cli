package com.shop.service;

interface Notifier {
    void notify(double amount);

    default void notify(String message, int level) {
        int amount = level;
        {
            int amount2 = amount;
            notify(amount2);
        }
        for (int i = 0, n = message.length(); i < n; i++) {
            notify(message, i);
        }
    }
}
