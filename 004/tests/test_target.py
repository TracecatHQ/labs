from __future__ import annotations

import importlib.util
import sqlite3
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def load_server():
    spec = importlib.util.spec_from_file_location("lab004_server", ROOT / "target" / "server.py")
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class ReadOnlyAuthorizerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.server = load_server()

    def connection(self) -> sqlite3.Connection:
        connection = sqlite3.connect(":memory:")
        connection.execute("CREATE TABLE logs (id INTEGER PRIMARY KEY, value TEXT)")
        connection.execute("INSERT INTO logs(value) VALUES ('ok')")
        connection.set_authorizer(self.server._authorizer)
        return connection

    def test_select_is_allowed(self) -> None:
        connection = self.connection()
        self.assertEqual(connection.execute("SELECT value FROM logs").fetchone()[0], "ok")

    def test_write_and_pragma_are_denied(self) -> None:
        connection = self.connection()
        with self.assertRaises(sqlite3.DatabaseError):
            connection.execute("DELETE FROM logs")
        with self.assertRaises(sqlite3.DatabaseError):
            connection.execute("PRAGMA table_info(logs)").fetchall()


if __name__ == "__main__":
    unittest.main()
