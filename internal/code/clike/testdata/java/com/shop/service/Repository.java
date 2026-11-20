package com.shop.service;

import java.util.ArrayList;
import java.util.List;

public interface Repository<T> {
    void save(T item);

    List<T> findAll();

    static <T> Repository<T> inMemory() {
        List<T> store = new ArrayList<>();
        return new Repository<>() {
            public void save(T item) {
                store.add(item);
            }

            public List<T> findAll() {
                return store;
            }
        };
    }
}
