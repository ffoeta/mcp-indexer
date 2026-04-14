from events import EventBus, make_logger_handler, make_filter_handler


class Dispatcher:
    def __init__(self, bus):
        self.bus = bus
        self._active = True

    def setup_logging(self, prefix):
        handler = make_logger_handler(prefix)
        self.bus.subscribe("all", handler)

    def setup_filter(self, predicate, next_handler):
        handler = make_filter_handler(predicate, next_handler)
        self.bus.subscribe("filtered", handler)

    def dispatch(self, event_type, data):
        if self._active:
            self.bus.publish(event_type, data)

    def stop(self):
        self._active = False

    def is_active(self):
        return self._active


def build_dispatcher():
    """Factory with a closure capturing the returned dispatcher."""
    bus = EventBus()
    d = Dispatcher(bus)

    def on_error(data):                   # замыкание — не будет в символах
        d.stop()

    bus.subscribe("error", on_error)
    return d
