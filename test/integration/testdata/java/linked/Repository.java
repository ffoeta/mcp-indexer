package linked;

import java.util.List;
import java.util.Optional;

public interface Repository<T> {
    Optional<T> findById(long id);
    List<T> findAll();
    void save(T entity);
    void delete(long id);
}
