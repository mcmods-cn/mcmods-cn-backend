#!/usr/bin/env python3
"""Fail closed before accepting readiness or stopping owned Linux test services."""
import json
import os
import signal
import socket
import sys
import time
import urllib.request
from pathlib import Path


class OwnershipError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise OwnershipError(message)


def owned_state(directory):
    p = Path(directory)
    require(p.is_absolute() and not p.is_symlink(), "Invalid test-services directory")
    require(p.name.startswith("mcmods-test-services."), "Unexpected test-services directory")
    require(p.resolve() == p and p.stat().st_uid == os.getuid(), "Unowned test-services directory")
    identity = json.loads((p / "identity.json").read_text())
    require(identity.get("uid") == os.getuid() and identity.get("directory") == str(p), "Unowned test-services identity")
    require(identity.get("database") == "mcmods_audit", "Unexpected isolated database")
    require((p / "owned-directory").read_text().strip() == str(p), "Unowned test-services marker")
    return p


def owns_listener(pid, port):
    """Match the exact loopback LISTEN inode to an fd belonging to this PID."""
    proc = Path(f"/proc/{pid}")
    inodes = set()
    for fd in (proc / "fd").iterdir():
        try:
            link = os.readlink(fd)
        except OSError:
            continue
        if link.startswith("socket:[") and link.endswith("]"):
            inodes.add(link[8:-1])
    endpoint = f"0100007F:{port:04X}"
    for row in (proc / "net/tcp").read_text().splitlines()[1:]:
        fields = row.split()
        if len(fields) > 9 and fields[1] == endpoint and fields[3] == "0A" and fields[9] in inodes:
            return True
    return False


def verify_process(p, service, require_launch=False, port=None):
    ports = {"redis": 56379, "nats": 54222, "typesense": 58108}
    require(service in ports, "Unknown isolated service")
    pid = int((p / f"{service}.pid").read_text())
    require(pid > 1, "Invalid isolated service PID")
    proc = Path(f"/proc/{pid}")
    require(proc.stat().st_uid == os.getuid(), "Unexpected isolated service owner")
    require((proc / "stat").read_text().rsplit(")", 1)[1].split()[0] != "Z", "Isolated service exited")
    launch = p / f"{service}-started.pid"
    # New starts must prove the PID is the newly launched child. For stopping
    # older task states, PID/config/UID/socket evidence is still mandatory.
    require(not require_launch or launch.is_file(), "Missing new service launch PID")
    if launch.exists():
        require(int(launch.read_text()) == pid, "Isolated service launch PID mismatch")
    conf = p / f"{service}.conf"
    require(conf.is_file() and not conf.is_symlink() and conf.stat().st_uid == os.getuid(), "Unexpected service configuration")
    if service != "redis":
        argv = (proc / "cmdline").read_bytes().split(b"\0")
        target = str(conf).encode()
        exact = b"--config=" + target in argv or any(
            arg in (b"-c", b"--config") and argv[index + 1:index + 2] == [target]
            for index, arg in enumerate(argv)
        )
        require(exact, "Isolated service configuration argument mismatch")
    require(owns_listener(pid, ports[service] if port is None else port), "Loopback listener does not belong to isolated service PID")
    return pid


def redis_identity(p, pid):
    # Redis rewrites argv. Its secret, INFO PID and exact config_file prove
    # identity alongside the listening fd, rather than trusting argv text.
    password = next(line.split(" ", 1)[1] for line in (p / "redis.conf").read_text().splitlines() if line.startswith("requirepass "))
    require(bool(password), "Missing owned Redis credential")
    with socket.create_connection(("127.0.0.1", 56379), timeout=2) as connection:
        with connection.makefile("rb") as stream:
            connection.sendall(f"*2\r\n$4\r\nAUTH\r\n${len(password)}\r\n{password}\r\n".encode())
            require(stream.readline() == b"+OK\r\n", "Owned Redis authentication failed")
            connection.sendall(b"*2\r\n$4\r\nINFO\r\n$6\r\nserver\r\n")
            header = stream.readline()
            require(header.startswith(b"$"), "Invalid owned Redis identity response")
            size = int(header[1:].strip())
            require(0 < size <= 65536, "Invalid owned Redis identity length")
            info = stream.read(size).decode()
            fields = dict(line.split(":", 1) for line in info.splitlines() if ":" in line)
            require(fields.get("process_id") == str(pid) and fields.get("config_file") == str(p / "redis.conf"), "Unexpected Redis identity")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def service_ready(p, service):
    deadline = time.monotonic() + (30 if service == "typesense" else 15)
    while True:
        try:
            pid = verify_process(p, service, require_launch=True)
            if service == "redis":
                redis_identity(p, pid)
            elif service == "nats":
                with socket.create_connection(("127.0.0.1", 54222), timeout=.5) as connection:
                    connection.recv(4096)
                    connection.sendall(b'CONNECT {"verbose":false}\r\nPING\r\n')
                    require(b"PONG" in connection.recv(4096), "Owned NATS did not reply")
            else:
                key = (p / "typesense-key").read_text().strip()
                require(bool(key), "Missing owned Typesense credential")
                request = urllib.request.Request("http://127.0.0.1:58108/health", headers={"X-TYPESENSE-API-KEY": key})
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
                with opener.open(request, timeout=1) as response:
                    require(response.status == 200, "Owned Typesense is not ready")
            verify_process(p, service, require_launch=True)
            return
        except (OSError, ValueError, OwnershipError):
            if time.monotonic() >= deadline:
                raise OwnershipError(f"New owned {service} process did not become ready") from None
            time.sleep(.1)


def stop_services(p):
    verified = []
    for service in ("redis", "nats", "typesense"):
        pidfile = p / f"{service}.pid"
        if not pidfile.exists():
            continue
        pid = int(pidfile.read_text())
        require(pid > 1, "Invalid isolated service PID")
        proc = Path(f"/proc/{pid}")
        if not proc.exists():
            continue
        try:
            require(proc.stat().st_uid == os.getuid(), "Unexpected isolated service owner")
            if (proc / "stat").read_text().rsplit(")", 1)[1].split()[0] == "Z":
                # An exited, same-UID child has no listener and must never be
                # signalled; it must not prevent stopping the owned PG cluster.
                continue
        except FileNotFoundError:
            continue  # The exited process was reaped between identity reads.
        verified_pid = verify_process(p, service)
        if service == "redis":
            redis_identity(p, verified_pid)
        verified.append((service, verified_pid))
    # Validate every present service before any signal. Recheck immediately
    # before signalling; fail closed if PID/config/socket evidence changes.
    for service, pid in verified:
        require(verify_process(p, service) == pid, "Isolated service PID changed")
    for _, pid in verified:
        os.kill(pid, signal.SIGTERM)


def main():
    require(len(sys.argv) == 3 and sys.argv[1] in ("ready", "stop", "verify-state"), "Usage: service-process-owner.py ready|stop|verify-state <owned-directory>")
    p = owned_state(sys.argv[2])
    if sys.argv[1] == "ready":
        services = ["redis", "nats"] + (["typesense"] if (p / "typesense-key").exists() else [])
        for service in services:
            service_ready(p, service)
        print("New owned test services are ready")
    elif sys.argv[1] == "stop":
        stop_services(p)
    # verify-state checks only directory/marker identity; no service health or
    # process ownership is implied. It performs no connections or signals.


if __name__ == "__main__":
    try:
        main()
    except (OwnershipError, OSError, ValueError, StopIteration):
        # Do not echo parsed configuration, credentials or arbitrary file data.
        print("Refusing unverified or unavailable owned test service", file=sys.stderr)
        raise SystemExit(1)
