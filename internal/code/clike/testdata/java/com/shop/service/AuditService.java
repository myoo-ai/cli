package com.shop.service;

import com.shop.model.Order;
import com.shop.model.Status;

class AuditService extends OrderService {
    AuditService() {
        super(Repository.inMemory());
    }

    @Override
    public Order checkout(String customer) {
        Order order = super.checkout(customer);
        Order.Builder builder = new Order.Builder();
        builder.item("audit", 0).build();
        boolean done = Status.PAID.isFinal();
        Repository.<Order>inMemory().save(order);
        com.shop.model.Item item = new com.shop.model.Item("x", 1);
        return order;
    }

    void audit(Order order) throws java.io.IOException {
        try (var in = new java.io.StringReader("x"); var out = new java.io.StringWriter()) {
            notify(1.0);
            this.describe(order);
        } catch (IllegalStateException | IllegalArgumentException e) {
            throw new java.io.IOException(e);
        } finally {
            order.add("audit", 0);
        }
    }

    static class Local {
        void run() {
            class Inner {
                int depth() {
                    return 1;
                }
            }
            Inner inner = new Inner();
            inner.depth();
            int caf\u00e9 = 1;
            café++;
        }
    }
}
