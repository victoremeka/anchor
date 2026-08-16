import os
import signal
import subprocess
import time
import urllib.request

ENGINE_BINARY = os.environ.get("ANCHOR_ENGINE_BINARY", "/tmp/anchor-engine")
DATABASE_URL = os.environ.get(
    "ANCHOR_DATABASE_URL", "postgresql://root@localhost:26257/anchor?sslmode=disable"
)


class ManagedEngine:
    def __init__(self, addr: str, lease_ttl_seconds: int = 60, linked_tables: str = "orders:order_id"):
        self.addr = addr
        self.base_url = f"http://{addr}"
        self.lease_ttl_seconds = lease_ttl_seconds
        self.linked_tables = linked_tables
        self.process = None

    def start(self, wait: bool = True) -> None:
        env = dict(os.environ)
        env["ANCHOR_DATABASE_URL"] = DATABASE_URL
        env["ANCHOR_LISTEN_ADDR"] = self.addr
        env["ANCHOR_LEASE_TTL_SECONDS"] = str(self.lease_ttl_seconds)
        env["ANCHOR_RECLAIM_SWEEP_INTERVAL_SECONDS"] = "2"
        env["ANCHOR_LINKED_TABLES"] = self.linked_tables
        self.process = subprocess.Popen(
            [ENGINE_BINARY], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT
        )
        if wait:
            self.wait_healthy()

    def wait_healthy(self, timeout: float = 10.0) -> None:
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                urllib.request.urlopen(f"{self.base_url}/healthz", timeout=0.5)
                return
            except Exception:
                time.sleep(0.2)
        raise RuntimeError(f"engine at {self.addr} did not become healthy in time")

    def kill(self) -> None:
        if self.process is None:
            return
        self.process.send_signal(signal.SIGKILL)
        self.process.wait(timeout=5)
        self.process = None

    def stop(self) -> None:
        if self.process is None:
            return
        self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
        self.process = None
