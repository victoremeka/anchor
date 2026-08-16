import csv
import io
import os
import subprocess

CONTAINER = os.environ.get("ANCHOR_CRDB_CONTAINER", "anchor-crdb")
DATABASE = os.environ.get("ANCHOR_CRDB_DATABASE", "anchor")


def query(sql: str) -> list:
    out = subprocess.run(
        ["docker", "exec", CONTAINER, "./cockroach", "sql", "--insecure", "-d", DATABASE, "--format=csv", "-e", sql],
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    return list(csv.DictReader(io.StringIO(out)))
