#!/usr/bin/env python3
"""Create disposable, labelled loopback services; never reset a supplied DB URL."""
import argparse
import errno
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import subprocess
import time
from urllib.request import ProxyHandler, build_opener
from urllib.parse import parse_qsl, urlsplit

REPOSITORY = Path(__file__).resolve().parents[2]
DEFAULT_STATE = REPOSITORY / ".audit-test-environment"
IMAGES = {
    "postgres": "public.ecr.aws/docker/library/postgres@sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722",
    "redis": "public.ecr.aws/docker/library/redis@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f",
    "nats": "public.ecr.aws/docker/library/nats@sha256:e4bf19f15fd3218814a4e3c9e0064e1334bd8aa20d5984b9f1a0afd084f8cc00",
}
LABEL = "mcmods.audit.owner"
TYPESENSE_IMAGE = "typesense/typesense@sha256:316b7e71c21f7e5e5caa8daa150e1b3f2be8c876081ee1f77bc2d92cd7f137d0"
NATIVE_BINARIES = {"redis": "/usr/local/bin/redis-server", "typesense": "/opt/typesense-server"}
SERVICE_PORTS = {"postgres": 5432, "redis": 6379, "nats": 4222, "typesense": 8108}


def docker(*arguments, capture=False, environment_overlay=None):
    environment = os.environ.copy()
    for key in ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"):
        environment.pop(key, None)
    environment.update(environment_overlay or {})
    return subprocess.run(
        ["docker", "--host=unix:///var/run/docker.sock", *arguments],
        env=environment, check=True, text=True,
        stdout=subprocess.PIPE if capture else None,
    ).stdout


def write_private(path, value):
    with open(path, "w", opener=lambda name, flags: os.open(name, flags, 0o600)) as target:
        target.write(value)
    path.chmod(0o600)


def verify_container(name, owner):
    description = json.loads(docker("inspect", name, capture=True))[0]
    if description["Config"].get("Labels", {}).get(LABEL) != owner:
        raise RuntimeError("refusing to operate on a container without this environment's owner label")
    return description


def read_owned_state(state):
    record = json.loads((state / "resources.json").read_text())
    environment = json.loads((state / "environment.json").read_text())
    if not re.fullmatch(r"[a-f0-9]{32}", record.get("owner", "")) or not re.fullmatch(r"test_mcmods_audit_[a-f0-9]{16}", record.get("database", "")):
        raise RuntimeError("state is not an owned isolated environment record")
    containers = record["containers"]
    if set(containers) not in ({"postgres", "redis", "nats"}, set(SERVICE_PORTS)):
        raise RuntimeError("owned environment service inventory is incomplete")
    endpoints = {}
    for name, identifier in containers.items():
        info = verify_container(identifier, record["owner"])
        bindings = info["NetworkSettings"]["Ports"].get(f"{SERVICE_PORTS[name]}/tcp")
        if not bindings or len(bindings) != 1 or bindings[0]["HostIp"] != "127.0.0.1":
            raise RuntimeError("owned services must have one loopback binding per endpoint")
        endpoints[name] = bindings[0]["HostPort"]
    try:
        database = urlsplit(environment["DATABASE_URL"])
        same_database = (database.scheme == "postgres" and database.hostname == "127.0.0.1"
                         and database.port == int(endpoints["postgres"]) and database.username == "mcmods"
                         and database.path == "/" + record["database"] and not database.fragment
                         and parse_qsl(database.query) == [("sslmode", "disable")]
                         and environment["DATABASE_URL"] == environment["MCMODS_TEST_DATABASE_URL"])
    except (KeyError, ValueError):
        same_database = False
    if not same_database:
        raise RuntimeError("configured PostgreSQL target does not match the owned PostgreSQL endpoint")
    if (environment.get("REDIS_ADDR") != "127.0.0.1:" + endpoints["redis"]
            or environment.get("NATS_URL") != "nats://127.0.0.1:" + endpoints["nats"]
            or environment.get("MCMODS_TEST_NATS_URL", environment["NATS_URL"]) != environment["NATS_URL"]):
        raise RuntimeError("configured Redis or NATS target is outside the owned environment")
    typesense = "http://127.0.0.1:" + endpoints["typesense"] if "typesense" in endpoints else None
    if environment.get("MCMODS_TEST_TYPESENSE_URL") != typesense:
        raise RuntimeError("configured Typesense target is outside the owned environment")
    return record, environment


def ready_typesense(address):
    client = build_opener(ProxyHandler({}))
    for _ in range(60):
        try:
            with client.open(address + "/health", timeout=1) as response:
                if json.load(response).get("ok") is True:
                    return
        except (OSError, ValueError):
            pass
        time.sleep(1)
    raise RuntimeError("owned Typesense service did not become ready")


def up(state, go_binary, real_services=False):
    if state.exists():
        raise RuntimeError("state directory already exists; use run or down instead of overwriting it")
    docker("info", "--format", "{{.ServerVersion}}", capture=True)
    state.mkdir(mode=0o700, parents=True)
    owner = secrets.token_hex(16)
    database = "test_mcmods_audit_" + secrets.token_hex(8)
    database_password = secrets.token_urlsafe(32)
    record = {"owner": owner, "database": database, "containers": {}}
    write_private(state / "resources.json", json.dumps(record, indent=2) + "\n")
    ports = {service: SERVICE_PORTS[service] for service in IMAGES}
    images = dict(IMAGES)
    commands = {"postgres": ["postgres", "-c", "max_locks_per_transaction=512"],
                "redis": ["redis-server", "--save", "", "--appendonly", "no"],
                "nats": ["--jetstream", "--store_dir", "/data"]}
    typesense_key = None
    if real_services:
        ports["typesense"] = 8108
        images["typesense"] = TYPESENSE_IMAGE
        commands["typesense"] = ["--data-dir", "/data", "--thread-pool-size", "2", "--num-collections-parallel-load", "2"]
        typesense_key = secrets.token_hex(32)
    for service in ports:
        name = "mcmods-audit-" + service + "-" + owner[:12]
        options = ["create", "--name", name, "--label", LABEL + "=" + owner,
                   "-p", "127.0.0.1::" + str(ports[service])]
        credentials = {}
        if service == "postgres":
            options += ["-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_USER=mcmods",
                        "-e", "POSTGRES_DB=" + database]
            credentials["POSTGRES_PASSWORD"] = database_password
        elif service == "typesense":
            options += ["-e", "TYPESENSE_API_KEY", "--tmpfs", "/data:rw,size=384m"]
            credentials["TYPESENSE_API_KEY"] = typesense_key
        container_id = docker(*options, images[service], *commands[service], capture=True,
                              environment_overlay=credentials).strip()
        record["containers"][service] = container_id
        write_private(state / "resources.json", json.dumps(record, indent=2) + "\n")
        docker("start", container_id, capture=True)
        info = verify_container(container_id, owner)
        binding = info["NetworkSettings"]["Ports"][str(ports[service]) + "/tcp"][0]
        if binding["HostIp"] != "127.0.0.1":
            raise RuntimeError("refusing a service not bound to loopback")
        ports[service] = int(binding["HostPort"])
    url = f"postgres://mcmods:{database_password}@127.0.0.1:{ports['postgres']}/{database}?sslmode=disable"
    environment = {
        "MCMODS_SKIP_DOTENV": "true",
        "APP_ENV": "test", "APP_ADDR": "127.0.0.1:8080", "DATABASE_URL": url,
        "MCMODS_TEST_DATABASE_URL": url,
        "JWT_SECRET": secrets.token_hex(32), "SETTINGS_ENCRYPTION_KEY": secrets.token_hex(32),
        "SEED_ADMIN_PASSWORD": secrets.token_urlsafe(32),
        "SEED_ADMIN_EMAIL": "admin@example.invalid",
        "ANTI_ABUSE_HMAC_SECRET": secrets.token_hex(32), "ANTI_ABUSE_IP_HASH_SECRET": secrets.token_hex(32),
        "REDIS_ENABLED": "true", "REDIS_ADDR": f"127.0.0.1:{ports['redis']}", "REDIS_NAMESPACE": owner,
        "NATS_ENABLED": "true", "NATS_URL": f"nats://127.0.0.1:{ports['nats']}",
        "NATS_JETSTREAM_ENABLED": "true", "NATS_SUBJECT_PREFIX": "audit." + owner,
        "NATS_JETSTREAM_STREAM": "AUDIT_" + owner.upper(), "SMTP_ENABLED": "false",
        "TYPESENSE_ENABLED": "false", "MCMODS_RUN_DB_INTEGRATION": "1", "MCMODS_RUN_ACTIVITY_LOAD": "1",
    }
    # Explicitly clear inherited provider credentials. SKIP_DOTENV also stops
    # the config loader from refilling these empty values from private files.
    for key in ("MODRINTH_TOKEN", "CURSEFORGE_API_KEY", "GITHUB_TOKEN", "SMTP_PASSWORD", "SMTP_USERNAME",
                "SMTP_HOST", "REDIS_USERNAME", "REDIS_PASSWORD", "NATS_TOKEN", "NATS_USERNAME", "NATS_PASSWORD", "TYPESENSE_API_KEY",
                "YGGDRASIL_PRIVATE_KEY_BASE64", "TURNSTILE_SECRET_KEY", "TURNSTILE_SITE_KEY"):
        environment[key] = ""
    if real_services:
        address = f"http://127.0.0.1:{ports['typesense']}"
        ready_typesense(address)
        for service, source in NATIVE_BINARIES.items():
            executable = state / Path(source).name
            docker("cp", f"{record['containers'][service]}:{source}", str(executable))
            executable.chmod(0o700)
            environment[f"MCMODS_TEST_{service.upper()}_BINARY"] = str(executable)
        environment.update({
            "MCMODS_AUDIT_PG_LOCK": str(state / "postgres-ddl.lock"),
            "MCMODS_RUN_REDIS_INTEGRATION": "1",
            "MCMODS_RUN_NATS_INTEGRATION": "1", "MCMODS_TEST_NATS_URL": environment["NATS_URL"],
            "MCMODS_RUN_TYPESENSE_INTEGRATION": "1", "MCMODS_TEST_TYPESENSE_URL": address,
            "MCMODS_TEST_TYPESENSE_API_KEY": typesense_key,
        })
    write_private(state / "environment.json", json.dumps(environment, indent=2) + "\n")
    write_private(state / "activate.sh", "".join(f"export {key}={shlex.quote(value)}\n" for key, value in environment.items()))
    # The entrypoint's temporary initialization server uses a Unix socket only.
    # Check our container's TCP listener before using its published host port.
    for attempt in range(60):
        try:
            docker("exec", record["containers"]["postgres"], "pg_isready", "-h", "127.0.0.1", "-U", "mcmods", "-d", database, capture=True)
            break
        except subprocess.CalledProcessError:
            if attempt == 59:
                raise
            time.sleep(1)
    _, verified_environment = read_owned_state(state)
    subprocess.run([go_binary, "run", "./cmd/db-initialize", "--bootstrap-admin"],
                   cwd=REPOSITORY, env=os.environ | verified_environment, check=True)
    print("Owned services and full schema initialized. Activate:", state / "activate.sh")


def run(state, command):
    if not command:
        raise RuntimeError("run requires a command after --")
    _, environment = read_owned_state(state)
    controlled = {"MCMODS_TEST_NATS_URL", "MCMODS_TEST_TYPESENSE_URL", "MCMODS_TEST_TYPESENSE_API_KEY",
                  "MCMODS_TEST_REDIS_BINARY", "MCMODS_TEST_TYPESENSE_BINARY"}
    inherited = {key: value for key, value in os.environ.items() if key not in controlled}
    return subprocess.run(command, cwd=REPOSITORY, env=inherited | environment).returncode


def down(state):
    record = json.loads((state / "resources.json").read_text())
    # Check every resource before removing any. Remove only recorded container
    # IDs and their own anonymous volumes; never a user-supplied path or DB.
    for container_id in record["containers"].values():
        verify_container(container_id, record["owner"])
    for container_id in record["containers"].values():
        docker("rm", "-f", "-v", container_id, capture=True)
    if "typesense" in record["containers"]:
        for source in NATIVE_BINARIES.values():
            (state / Path(source).name).unlink(missing_ok=True)
        (state / "postgres-ddl.lock").unlink(missing_ok=True)
    for filename in ("resources.json", "environment.json", "activate.sh"):
        path = state / filename
        if path.exists():
            path.unlink()
    try:
        state.rmdir()
    except OSError as error:
        if error.errno not in (errno.ENOTEMPTY, errno.EEXIST):
            raise
        print("Retained state directory containing other files; backups were not removed.")
    print("Removed only this environment's labelled disposable resources.")


def rebuild(state, replacement, go_binary):
    record, _ = read_owned_state(state)
    identity = docker(
        "exec", record["containers"]["postgres"], "psql", "-XAt", "-U", "mcmods", "-d", record["database"],
        "-c", "select current_database(),pg_get_userbyid(datdba) from pg_database where datname=current_database()",
        capture=True,
    ).strip()
    if identity != record["database"] + "|mcmods":
        raise RuntimeError("PostgreSQL database name or owner does not match the recorded test environment")
    up(replacement, go_binary, real_services="typesense" in record["containers"])
    print("The old environment and backups remain available until you run down on its state.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, default=DEFAULT_STATE)
    parser.add_argument("--go", default="go", help="installed project-compatible Go executable")
    parser.add_argument("--real-services", action="store_true", help="also provision Typesense and native test executables on Linux")
    parser.add_argument("--replacement", type=Path, help="new state directory for a rebuilt empty environment")
    parser.add_argument("action", choices=("up", "run", "down", "rebuild"))
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.action == "up":
        up(args.state.resolve(), args.go, args.real_services)
    elif args.action == "down":
        down(args.state.resolve())
    elif args.action == "rebuild":
        if not args.replacement:
            parser.error("rebuild requires --replacement with an unused state directory")
        rebuild(args.state.resolve(), args.replacement.resolve(), args.go)
    else:
        command = args.command[1:] if args.command[:1] == ["--"] else args.command
        raise SystemExit(run(args.state.resolve(), command))


if __name__ == "__main__":
    main()
