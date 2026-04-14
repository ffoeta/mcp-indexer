import logging

logger = logging.getLogger(__name__)


class EventBus:
    def __init__(self):
        self._handlers = {}

    def subscribe(self, event_type, handler):
        self._handlers.setdefault(event_type, []).append(handler)

    def publish(self, event_type, data):
        for handler in self._handlers.get(event_type, []):
            handler(data)

    def clear(self, event_type=None):
        if event_type:
            self._handlers.pop(event_type, None)
        else:
            self._handlers.clear()


def make_logger_handler(prefix):
    """Returns a closure — logs events with prefix.
    Inner function 'handle' is a closure; it is NOT indexed as a symbol.
    """
    def handle(data):                     # замыкание — не будет в символах
        logger.info("%s: %s", prefix, data)
    return handle


def make_filter_handler(predicate, next_handler):
    """Returns a closure that filters before forwarding.
    Inner function 'handle' is a closure; it is NOT indexed as a symbol.
    """
    def handle(data):                     # замыкание — не будет в символах
        if predicate(data):
            next_handler(data)
    return handle
