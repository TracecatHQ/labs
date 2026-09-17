"""Read-only HTTP facade over the public Simbian sample in SQLite."""

from __future__ import annotations

import json
import os
import sqlite3
import threading
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ARCHIVE = Path(os.environ.get("LAB_004_ARCHIVE", "/app/assets/sample-data.zip"))
SCHEMA_PATH = Path(os.environ.get("LAB_004_SCHEMA", "/app/assets/log_schema.json"))
DATABASE = Path(os.environ.get("LAB_004_DATABASE", "/data/logs.sqlite"))
ROW_LIMIT = 10
MAX_BODY = 16_384
MAX_SQL = 4_000


def _quoted(name: str) -> str:
    return '"' + name.replace('"', '""') + '"'


def build_database() -> None:
    if DATABASE.exists():
        return
    DATABASE.parent.mkdir(parents=True, exist_ok=True)
    temporary = DATABASE.with_suffix(".tmp")
    temporary.unlink(missing_ok=True)
    schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
    fields = []
    seen = {"id", "raw_json"}
    for field in sorted(schema["fields"]):
        if field.lower() not in seen:
            seen.add(field.lower())
            fields.append(field)
    aliases = {key: tuple(value) for key, value in schema.get("aliases", {}).items()}

    connection = sqlite3.connect(temporary)
    connection.execute("PRAGMA journal_mode=OFF")
    connection.execute("PRAGMA synchronous=OFF")
    columns = ", ".join(f"{_quoted(field)} TEXT" for field in fields)
    connection.execute(
        f"CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, {columns}, raw_json TEXT)"
    )
    names = ", ".join(_quoted(field) for field in fields)
    placeholders = ", ".join("?" for _ in fields)
    insert = f"INSERT INTO logs ({names}, raw_json) VALUES ({placeholders}, ?)"

    def extract(record: dict, field: str) -> str:
        if field in record:
            return str(record[field])
        matches = [alias for alias in aliases.get(field, ()) if alias in record]
        if len(matches) > 1:
            raise ValueError(f"multiple aliases for {field}: {matches}")
        return str(record[matches[0]]) if matches else ""

    with zipfile.ZipFile(ARCHIVE) as archive:
        payload = json.loads(archive.read("sample.json"))
    batch = []
    for record in payload["logs"]:
        batch.append(
            tuple(extract(record, field) for field in fields)
            + (json.dumps(record, separators=(",", ":"), default=str),)
        )
        if len(batch) == 500:
            connection.executemany(insert, batch)
            batch.clear()
    if batch:
        connection.executemany(insert, batch)
    for field in schema.get("index_fields", []):
        if field in fields:
            connection.execute(
                f"CREATE INDEX {_quoted('idx_' + field)} ON logs({_quoted(field)})"
            )
    connection.commit()
    connection.close()
    temporary.replace(DATABASE)


def _authorizer(operation: int, _arg1: str, _arg2: str, _db: str, _source: str) -> int:
    allowed = {
        sqlite3.SQLITE_SELECT,
        sqlite3.SQLITE_READ,
        sqlite3.SQLITE_FUNCTION,
        sqlite3.SQLITE_RECURSIVE,
    }
    return sqlite3.SQLITE_OK if operation in allowed else sqlite3.SQLITE_DENY


class Handler(BaseHTTPRequestHandler):
    server_version = "lab004-query/1"

    def log_message(self, format: str, *args: object) -> None:
        print(f"{self.address_string()} {format % args}", flush=True)

    def reply(self, status: int, payload: dict) -> None:
        body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        if self.path == "/ready":
            with sqlite3.connect(DATABASE) as connection:
                count = connection.execute("SELECT count(*) FROM logs").fetchone()[0]
            self.reply(200, {"ready": True, "rows": count, "row_limit": ROW_LIMIT})
            return
        if self.path == "/schema":
            schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
            self.reply(
                200,
                {
                    "table": "logs",
                    "columns": ["id", *schema["fields"], "raw_json"],
                    "row_limit": ROW_LIMIT,
                    "examples": [
                        'SELECT * FROM logs WHERE "EventID" = \'1\' LIMIT 10',
                        'SELECT DISTINCT "Hostname" FROM logs LIMIT 10',
                        'SELECT * FROM logs WHERE "CommandLine" LIKE \'%powershell%\' LIMIT 10',
                    ],
                },
            )
            return
        self.reply(404, {"error": "not_found"})

    def do_POST(self) -> None:
        if self.path != "/query":
            self.reply(404, {"error": "not_found"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if size <= 0 or size > MAX_BODY:
                raise ValueError("invalid request size")
            query = str(json.loads(self.rfile.read(size))["query"]).strip()
            if not query or len(query) > MAX_SQL:
                raise ValueError("query must contain 1-4000 characters")
            if query.upper() == "SCHEMA":
                schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
                self.reply(
                    200,
                    {
                        "table": "logs",
                        "columns": ["id", *schema["fields"], "raw_json"],
                        "row_limit": ROW_LIMIT,
                        "examples": [
                            'SELECT * FROM logs WHERE "EventID" = \'1\' LIMIT 10',
                            'SELECT DISTINCT "Hostname" FROM logs LIMIT 10',
                            'SELECT * FROM logs WHERE "CommandLine" LIKE \'%powershell%\' LIMIT 10',
                        ],
                    },
                )
                return
            connection = sqlite3.connect(f"file:{DATABASE}?mode=ro", uri=True, timeout=5)
            connection.row_factory = sqlite3.Row
            connection.set_authorizer(_authorizer)
            remaining = 1_000_000

            def progress() -> int:
                nonlocal remaining
                remaining -= 1
                return 1 if remaining <= 0 else 0

            connection.set_progress_handler(progress, 1000)
            cursor = connection.execute(query)
            rows = [dict(row) for row in cursor.fetchmany(ROW_LIMIT + 1)]
            connection.close()
            truncated = len(rows) > ROW_LIMIT
            self.reply(
                200,
                {
                    "rows": rows[:ROW_LIMIT],
                    "rows_shown": min(len(rows), ROW_LIMIT),
                    "truncated": truncated,
                },
            )
        except (KeyError, TypeError, ValueError, json.JSONDecodeError) as error:
            self.reply(400, {"error": type(error).__name__, "message": str(error)})
        except sqlite3.Error as error:
            self.reply(422, {"error": type(error).__name__, "message": str(error)})


def main() -> None:
    build_database()
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
    server.daemon_threads = True
    server.serve_forever()


if __name__ == "__main__":
    main()
