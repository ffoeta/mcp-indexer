package linked;

import java.util.List;
import java.util.ArrayList;
import java.util.Optional;
import java.util.Map;
import java.util.HashMap;

// static import индексируется как полное qualified имя (tree-sitter не различает static/non-static)
import static java.util.Collections.unmodifiableList;

public class OrderService {
    // статическое поле — парсер его НЕ извлекает
    public static final String DEFAULT_STATUS = "PENDING";

    private final Repository<Map<String, Object>> repository;
    private final EventBus eventBus;

    public OrderService(Repository<Map<String, Object>> repository) {
        this.repository = repository;
        this.eventBus = EventBus.create();
    }

    public Map<String, Object> createOrder(long customerId, List<String> items) {
        Map<String, Object> order = new HashMap<>();
        order.put("customerId", customerId);
        order.put("items", unmodifiableList(new ArrayList<>(items)));
        order.put("status", DEFAULT_STATUS);
        repository.save(order);
        eventBus.publish("order.created");
        return order;
    }

    public Optional<Map<String, Object>> getOrder(long id) {
        return repository.findById(id);
    }

    public List<Map<String, Object>> listOrders() {
        return repository.findAll();
    }

    public void cancelOrder(long id) {
        repository.delete(id);
        eventBus.publish("order.cancelled");
    }

    // статический factory
    public static OrderService withInMemoryRepo() {
        return new OrderService(new InMemoryRepository());
    }
}

class InMemoryRepository implements Repository<Map<String, Object>> {
    private final Map<Long, Map<String, Object>> store = new HashMap<>();
    private long nextId = 1;

    public Optional<Map<String, Object>> findById(long id) {
        return Optional.ofNullable(store.get(id));
    }

    public List<Map<String, Object>> findAll() {
        return new ArrayList<>(store.values());
    }

    public void save(Map<String, Object> entity) {
        store.put(nextId++, entity);
    }

    public void delete(long id) {
        store.remove(id);
    }
}
