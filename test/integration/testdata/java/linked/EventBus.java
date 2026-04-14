package linked;

import java.util.List;
import java.util.ArrayList;
import java.util.Map;
import java.util.HashMap;

// static import индексируется как полное qualified имя (tree-sitter не различает static/non-static)
import static java.util.Collections.emptyList;

public class EventBus {
    // статическое поле — парсер его НЕ извлекает
    public static final int MAX_HANDLERS = 64;

    private final Map<String, List<Runnable>> handlers = new HashMap<>();

    // статический factory — извлекается как метод класса
    public static EventBus create() {
        return new EventBus();
    }

    public void subscribe(String eventType, Runnable handler) {
        handlers.computeIfAbsent(eventType, k -> new ArrayList<>()).add(handler);
    }

    public void publish(String eventType) {
        List<Runnable> list = handlers.getOrDefault(eventType, emptyList());
        for (Runnable h : list) {
            h.run();
        }
    }

    public void clear(String eventType) {
        handlers.remove(eventType);
    }

    // ещё один статический метод
    public static boolean isValidEventType(String type) {
        return type != null && !type.isEmpty();
    }
}
