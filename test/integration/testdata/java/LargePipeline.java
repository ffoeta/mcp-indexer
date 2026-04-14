import java.util.List;
import java.util.ArrayList;
import java.util.Map;
import java.util.HashMap;
import java.util.Optional;
import java.io.IOException;

interface Processor {
    List<Object> process(Object data) throws IOException;
    boolean validate(Object data);
    String getName();
}

abstract class BaseProcessor implements Processor {
    protected final String name;
    protected final Map<String, Object> config;

    public BaseProcessor(String name) {
        this.name = name;
        this.config = new HashMap<>();
    }

    public boolean validate(Object data) {
        return data != null;
    }

    public String getName() {
        return name;
    }

    public void configure(String key, Object value) {
        config.put(key, value);
    }

    public Map<String, Object> getConfig() {
        return config;
    }
}

class FileProcessor extends BaseProcessor {
    private final String root;
    private final List<String> results;

    public FileProcessor(String name, String root) {
        super(name);
        this.root = root;
        this.results = new ArrayList<>();
    }

    public List<Object> process(Object data) throws IOException {
        validate(data);
        return new ArrayList<>(results);
    }

    public String getRoot() {
        return root;
    }

    public Optional<String> findFirst() {
        return results.stream().findFirst();
    }

    public Map<String, Object> getStats() {
        Map<String, Object> stats = new HashMap<>();
        stats.put("root", root);
        stats.put("count", results.size());
        return stats;
    }

    public void clear() {
        results.clear();
    }
}

class JsonProcessor extends FileProcessor {
    private Map<String, Object> schema;

    public JsonProcessor(String root) {
        super("json", root);
        this.schema = new HashMap<>();
    }

    public void setSchema(Map<String, Object> schema) {
        this.schema = schema;
    }

    public Map<String, Object> getSchema() {
        return schema;
    }

    public boolean validate(Object data) {
        if (!super.validate(data)) {
            return false;
        }
        return data instanceof Map || data instanceof List;
    }
}

public class PipelineOrchestrator {
    private final List<Processor> processors;
    private final Map<String, Object> results;
    private long startTime;

    public PipelineOrchestrator(List<Processor> processors) {
        this.processors = new ArrayList<>(processors);
        this.results = new HashMap<>();
    }

    public Map<String, Object> run(Object data) throws IOException {
        startTime = System.currentTimeMillis();
        for (Processor proc : processors) {
            if (proc.validate(data)) {
                results.put(proc.getName(), proc.process(data));
            }
        }
        return results;
    }

    public Optional<Object> getResult(String name) {
        return Optional.ofNullable(results.get(name));
    }

    public void reset() {
        results.clear();
        startTime = 0;
    }

    public long elapsedMs() {
        return System.currentTimeMillis() - startTime;
    }

    public Map<String, Object> summary() {
        Map<String, Object> s = new HashMap<>();
        s.put("processors", processors.size());
        s.put("results", results.size());
        s.put("elapsedMs", elapsedMs());
        return s;
    }
}
