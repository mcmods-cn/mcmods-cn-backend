#!/usr/bin/env python3
"""Create disposable, labelled loopback services; never reset a supplied DB URL."""
import argparse
import json
import os
from pathlib import Path
import secrets
import shlex
import subprocess
import time

REPOSITORY = Path(__file__).resolve().parents[2]
DEFAULT_STATE = REPOSITORY / ".audit-test-environment"
IMAGES = {
    "postgres": "public.ecr.aws/docker/library/postgres@sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722",
    "redis": "public.ecr.aws/docker/library/redis@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f",
    "nats": "public.ecr.aws/docker/library/nats@sha256:e4bf19f15fd3218814a4e3c9e0064e1334bd8aa20d5984b9f1a0afd084f8cc00",
}
LABEL = "mcmods.audit.owner"


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


def up(state, go_binary):
    if state.exists():
        raise RuntimeError("state directory already exists; use run or down instead of overwriting it")
    docker("info", "--format", "{{.ServerVersion}}", capture=True)
    state.mkdir(mode=0o700, parents=True)
    owner = secrets.token_hex(16)
    database = "test_mcmods_audit_" + secrets.token_hex(8)
    database_password = secrets.token_urlsafe(32)
    record = {"owner": owner, "database": database, "containers": {}}
    write_private(state / "resources.json", json.dumps(record, indent=2) + "\n")
    ports = {"postgres": 5432, "redis": 6379, "nats": 4222}
    for service in ports:
        name = "mcmods-audit-" + service + "-" + owner[:12]
        options = ["run", "-d", "--name", name, "--label", LABEL + "=" + owner,
                   "-p", "127.0.0.1::" + str(ports[service])]
        if service == "postgres":
            options += ["-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_USER=mcmods",
                        "-e", "POSTGRES_DB=" + database]
        command = {"postgres": ["postgres", "-c", "max_locks_per_transaction=512"], "redis": ["redis-server", "--save", "", "--appendonly", "no"],
                   "nats": ["--jetstream", "--store_dir", "/data"]}[service]
        container_id = docker(*options, IMAGES[service], *command, capture=True,
                              environment_overlay={"POSTGRES_PASSWORD": database_password} if service == "postgres" else None).strip()
        record["containers"][service] = container_id
        write_private(state / "resources.json", json.dumps(record, indent=2) + "\n")
        info = verify_container(container_id, owner)
        binding = info["NetworkSettings"]["Ports"][str(ports[service]) + "/tcp"][0]
        if binding["HostIp"] != "127.0.0.1":
            raise RuntimeError("refusing a service not bound to loopback")
        ports[service] = int(binding["HostPort"])
    url = f"postgres://mcmods:{database_password}@127.0.0.1:{ports['postgres']}/{database}?sslmode=disable"
    environment = {
        "MCMODS_SKIP_DOTENV": "true",
        "APP_ENV": "test", "APP_ADDR": "127.0.0.1:8080", "DATABASE_URL": url,
        "MCMODS_TEST_DATABASE_URL": url, "DB_RESET_ON_START": "false", "DB_RESET_CONFIRM": "",
        "JWT_SECRET": secrets.token_hex(32), "SETTINGS_ENCRYPTION_KEY": secrets.token_hex(32),
        "SEED_ADMIN_PASSWORD": secrets.token_urlsafe(32),
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
    write_private(state / "environment.json", json.dumps(environment, indent=2) + "\n")
    write_private(state / "activate.sh", "".join(f"export {key}={shlex.quote(value)}\n" for key, value in environment.items()))
    # pg_isready checks only our newly created container, never an inherited URL.
    for attempt in range(60):
        try:
            docker("exec", record["containers"]["postgres"], "pg_isready", "-U", "mcmods", "-d", database, capture=True)
            break
        except subprocess.CalledProcessError:
            if attempt == 59:
                raise
            time.sleep(1)
    init_env = os.environ | environment | {"APP_ENV": "development", "DB_RESET_ON_START": "true",
                                          "DB_RESET_CONFIRM": "RESET " + database}
    subprocess.run([go_binary, "run", "./cmd/db-reset"], cwd=REPOSITORY, env=init_env, check=True)
    print("Owned services and full schema initialized. Activate:", state / "activate.sh")


def run(state, command):
    if not command:
        raise RuntimeError("run requires a command after --")
    record = json.loads((state / "resources.json").read_text())
    for container_id in record["containers"].values():
        verify_container(container_id, record["owner"])
    environment = json.loads((state / "environment.json").read_text())
    return subprocess.run(command, cwd=REPOSITORY, env=os.environ | environment).returncode


def down(state):
    record = json.loads((state / "resources.json").read_text())
    # Check every resource before removing any. Remove only recorded container
    # IDs and their own anonymous volumes; never a user-supplied path or DB.
    for container_id in record["containers"].values():
        verify_container(container_id, record["owner"])
    for container_id in record["containers"].values():
        docker("rm", "-f", "-v", container_id, capture=True)
    for filename in ("resources.json", "environment.json", "activate.sh"):
        path = state / filename
        if path.exists():
            path.unlink()
    state.rmdir()
    print("Removed only this environment's labelled disposable resources.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, default=DEFAULT_STATE)
    parser.add_argument("--go", default="go", help="installed project-compatible Go executable")
    parser.add_argument("action", choices=("up", "run", "down"))
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.action == "up":
        up(args.state.resolve(), args.go)
    elif args.action == "down":
        down(args.state.resolve())
    else:
        command = args.command[1:] if args.command[:1] == ["--"] else args.command
        raise SystemExit(run(args.state.resolve(), command))


if __name__ == "__main__":
    main()
