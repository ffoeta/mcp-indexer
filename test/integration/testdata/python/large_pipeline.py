import os
import json
import logging
import re
import datetime
from pathlib import Path
from collections import defaultdict
from typing import List, Optional

logger = logging.getLogger(__name__)


class BaseProcessor:
    def __init__(self, name: str):
        self.name = name
        self.logger = logging.getLogger(name)

    def process(self, data):
        raise NotImplementedError

    def validate(self, data) -> bool:
        return data is not None

    def log_start(self):
        self.logger.info("starting %s", self.name)


class FileProcessor(BaseProcessor):
    def __init__(self, name: str, root: str):
        super().__init__(name)
        self.root = Path(root)
        self._cache = defaultdict(list)

    def process(self, data):
        self.log_start()
        return self._read_files(data)

    def _read_files(self, pattern: str):
        results = []
        for p in self.root.glob(pattern):
            results.append(p.read_text())
        return results

    def get_stats(self) -> dict:
        return {
            "root": str(self.root),
            "cached": len(self._cache),
        }


class JsonProcessor(FileProcessor):
    def __init__(self, root: str):
        super().__init__("json", root)
        self.schema = {}

    def process(self, data):
        raw = super().process(data)
        return [json.loads(r) for r in raw]

    def set_schema(self, schema: dict):
        self.schema = schema

    def validate(self, data) -> bool:
        if not super().validate(data):
            return False
        return isinstance(data, (dict, list))


class LogProcessor(BaseProcessor):
    LOG_PATTERN = re.compile(r"(\d{4}-\d{2}-\d{2}) (\w+) (.*)")

    def __init__(self):
        super().__init__("log")
        self.entries = []

    def process(self, data):
        self.log_start()
        for line in data.splitlines():
            m = self.LOG_PATTERN.match(line)
            if m:
                self.entries.append({
                    "date": m.group(1),
                    "level": m.group(2),
                    "msg": m.group(3),
                })
        return self.entries

    def filter_by_level(self, level: str):
        return [e for e in self.entries if e["level"] == level]

    def to_json(self) -> str:
        return json.dumps(self.entries, indent=2)


class PipelineOrchestrator:
    def __init__(self, processors: List[BaseProcessor]):
        self.processors = processors
        self._results = {}
        self._start_time = None

    def run(self, data):
        self._start_time = datetime.datetime.now()
        for proc in self.processors:
            if proc.validate(data):
                self._results[proc.name] = proc.process(data)
        return self._results

    def get_result(self, name: str) -> Optional[dict]:
        return self._results.get(name)

    def elapsed(self) -> float:
        if self._start_time is None:
            return 0.0
        delta = datetime.datetime.now() - self._start_time
        return delta.total_seconds()

    def reset(self):
        self._results = {}
        self._start_time = None

    def summary(self) -> dict:
        return {
            "processors": [p.name for p in self.processors],
            "results": list(self._results.keys()),
            "elapsed": self.elapsed(),
        }


def build_pipeline(root: str) -> PipelineOrchestrator:
    processors = [
        FileProcessor("files", root),
        JsonProcessor(root),
        LogProcessor(),
    ]
    return PipelineOrchestrator(processors)


def load_config(path: str) -> dict:
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_results(results: dict, out_path: str):
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(results, f, indent=2)
