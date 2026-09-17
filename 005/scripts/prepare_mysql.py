#!/usr/bin/env python3
"""Generate MySQL init files from the pinned SecRL anonymized CSV archive."""

import argparse
import csv
import json
from pathlib import Path

INCIDENTS = (5, 34, 38, 39, 55, 134, 166, 322)
SKIP = {"AzureDiagnostics", "LAQueryLogs"}


def quote(name: str) -> str:
    return "`" + name.replace("`", "``") + "`"


def type_map(path: Path) -> dict[str, str]:
    meta = path.with_suffix(".meta")
    if meta.exists():
        raw = json.loads(meta.read_text())
        if isinstance(raw, dict) and "columns" in raw and "dtypes" in raw:
            return dict(zip(raw["columns"], raw["dtypes"]))
        if isinstance(raw, dict):
            return raw
    with path.open(encoding="utf-8-sig", newline="") as handle:
        header = next(csv.reader(handle, delimiter="❖", quotechar='"'))
    return {column: "string" for column in header}


def files_for_table(incident: Path):
    for child in sorted(incident.iterdir()):
        if child.name.startswith(("._", ".DS_Store")):
            continue
        if child.is_file() and child.suffix == ".csv":
            yield child.stem, [child]
        elif child.is_dir():
            files = sorted(child.glob("*.csv"))
            if files:
                yield child.name, files


def build(incident: Path, output: Path) -> None:
    sql = [
        "CREATE DATABASE IF NOT EXISTS env_monitor_db;",
        "USE env_monitor_db;",
    ]
    for table, files in files_for_table(incident):
        if table in SKIP:
            continue
        columns = type_map(files[0])
        sql_type = {"long": "TEXT", "datetime": "TEXT", "bool": "TEXT", "dynamic": "LONGTEXT", "string": "TEXT"}
        definitions = ",\n  ".join(f"{quote(name)} {sql_type.get(kind, 'TEXT')}" for name, kind in columns.items())
        sql.append(f"CREATE TABLE {quote(table)} (\n  {definitions}\n);")
        for file in files:
            relative = file.relative_to(incident).as_posix().replace("'", "''")
            sql.append(
                f"LOAD DATA INFILE '/var/lib/mysql-files/{relative}' INTO TABLE {quote(table)} "
                "CHARACTER SET utf8mb4 FIELDS TERMINATED BY '❖' ENCLOSED BY '\"' LINES TERMINATED BY '\\n' IGNORE 1 ROWS;"
            )
    sql.extend([
        "CREATE USER IF NOT EXISTS 'lab_reader'@'%' IDENTIFIED BY 'lab-reader-only';",
        "GRANT SELECT, SHOW VIEW ON env_monitor_db.* TO 'lab_reader'@'%';",
        "ALTER USER 'lab_reader'@'%' WITH MAX_QUERIES_PER_HOUR 2000 MAX_USER_CONNECTIONS 8;",
        "FLUSH PRIVILEGES;",
    ])
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text("\n\n".join(sql) + "\n")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("data_root", type=Path, help="Extracted data_anonymized directory")
    parser.add_argument("output_root", type=Path)
    args = parser.parse_args()
    incidents_root = args.data_root / "incidents"
    for incident in INCIDENTS:
        source = incidents_root / f"incident_{incident}"
        if not source.is_dir():
            raise SystemExit(f"missing {source}")
        build(source, args.output_root / f"incident_{incident}.sql")


if __name__ == "__main__":
    main()
