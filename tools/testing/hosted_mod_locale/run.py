import json
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

os.umask(0o077)
workspace = Path(os.environ["GITHUB_WORKSPACE"])
temp = Path(os.environ["RUNNER_TEMP"])
stage = temp / "native-mod-browser"
stage.mkdir()
source = Path(__file__).parent
for name in ("browser.mjs", "mod-flow.mjs", "ssr-local.mjs"):
    shutil.copyfile(source / name, stage / name)
backend = workspace / "mcmods-cn-backend"
frontend = workspace / "mcmods-cn-frontend"
fixtures = temp / "mod-localization-fixture"
(stage / "node_modules").symlink_to(frontend / "node_modules", target_is_directory=True)
output = workspace / "safe-evidence"
output.mkdir(exist_ok=True)
case = "TestAuditLiveModFormFixture"
env = dict(os.environ)
env.update(MCMODS_RUN_LIVE_MOD_FORM="1", MCMODS_AUDIT_MOD_FORM_MODE="localizations-only",
           MCMODS_RUN_DB_INTEGRATION="1", MCMODS_AUDIT_MOD_FORM_DIR=str(fixtures),
           MCMODS_AUDIT_FRONTEND_REPO=str(frontend), MCMODS_AUDIT_EXPORTER_REPO=str(workspace/"mcmods_exporter"))
summary = {"goTest": case, "nativeExit": None, "goExit": None, "postgresSeal": False,
           "scope": "32 ordinary predeclared localized Mod creations, independent real HTTP reviews, PostgreSQL terminal state, production-built Next and headless Chromium on this disposable host"}
def write(name, value):
    (output / name).write_text(json.dumps(value, indent=2) + "\n")
def revisions(pins):
    return {name: value["commit"] for name, value in pins.items()}
def expected():
    return {p.name: subprocess.check_output(["git", "-C", p, "rev-parse", "HEAD"], text=True).strip()
            for p in (backend, frontend, workspace / "mcmods_exporter")}
summary["revisions"] = expected()
summary["goVersion"] = subprocess.check_output(["go", "version"], text=True).strip()
summary["nodeVersion"] = subprocess.check_output(["node", "--version"], text=True).strip()
write("summary.json", summary)
with (temp / "mod-form-private-go.jsonl").open("w+") as raw:
    fixture = subprocess.Popen(["go", "test", "-json", "-count=1", "-timeout=70m", "-run", "^" + case + "$", "./internal/httpapi"], cwd=backend, env=env, stdout=raw, stderr=raw)
    try:
        while not (fixtures / "runtime.json").exists() and fixture.poll() is None:
            time.sleep(0.5)
        if fixture.poll() is None:
            runtime = json.loads((fixtures / "runtime.json").read_text())
            assert revisions(runtime["repoRevisions"]) == summary["revisions"]
            chrome = subprocess.run(["node", stage/"browser.mjs", fixtures, output/"native.json", frontend/".next/standalone"], env=env, timeout=55*60,
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            summary["nativeExit"] = chrome.returncode
    finally:
        if fixture.poll() is None:
            (fixtures / ("done.flag" if summary["nativeExit"] == 0 else "STOP")).touch()
        summary["goExit"] = fixture.wait()
        raw.flush()
        raw.seek(0)
        terminal = []
        for line in raw:
            try: event = json.loads(line)
            except ValueError: continue
            if event.get("Action") in ("pass", "fail", "skip") and event.get("Test") == case:
                terminal.append({"action": event["Action"], "seconds": event.get("Elapsed")})
        summary["goTerminal"] = terminal
        write("summary.json", summary)
seal = fixtures / "sql-terminal-seal.json"
if seal.exists():
    db = json.loads(seal.read_text())
    checked = [{key: item[key] for key in ("caseId", "defaultLocale", "published", "independentDecisions", "businessPOSTs")} for item in db["checks"]]
    assert len(checked) == 32 and all(r["published"] == r["independentDecisions"] == 1 for r in checked)
    summary["postgresSeal"] = db["policyUnchanged"] and db["localizationCases"] == 32 and db["revisionCases"] == 0
    summary["postgresStatus"] = db["status"]
    assert revisions(db["repoRevisions"]) == summary["revisions"]
    write("postgres-seal.json", checked)
write("summary.json", summary)
if not (summary["nativeExit"] == summary["goExit"] == 0 and terminal and terminal[-1]["action"] == "pass" and summary["postgresSeal"]):
    sys.exit(1)
