#!/usr/bin/env python3
"""Read-only canonical references and PNG bytes for the owned geometry fixture.

Usage: python3 diagnose-geometry-canonical.py RUNTIME_JSON NEW_OUTPUT_DIR
Requires Pillow and the ownership-guarded diagnose-geometry.py beside this file.
This is synthetic contract/HTTP/PostgreSQL evidence, not game or queue evidence.
"""
import datetime
import hashlib
import io
import json
import pathlib
import re
import runpy
import sys
import urllib.parse
import urllib.request

from PIL import Image

namespace = runpy.run_path(str(pathlib.Path(__file__).with_name("diagnose-geometry.py")))
runtime = namespace["r"]
output = namespace["out"]
schema = namespace["schema"]
assert runtime["assetsIncluded"] is True
revision = runtime["revisionId"]
assert re.fullmatch(r"[0-9a-f-]{36}", revision)

# Query authoritative snapshot identities rather than accepting renderer IDs
# as their own expected values. The prior driver validated schema ownership,
# process ownership and the actual ready source/import/independent reviewer.
raw = namespace["sql"](f"""
begin transaction isolation level repeatable read read only;
set local search_path={schema},public;
select jsonb_agg(jsonb_build_object(
 'publicId',entity.public_id,'kind',resource.kind_code,
 'canonicalId',resource.canonical_id,'iconPath',snapshot.icon_path,
 'revisionId',revision.id,'active',revision.is_active,
 'versionPublicId',version.public_id,'modSiteId',mod.slug)
 order by resource.kind_code)
from game_resources resource
join catalog_entities entity on entity.id=resource.entity_id
join resource_import_snapshots snapshot on snapshot.resource_id=resource.entity_id
join catalog_import_revisions revision on revision.id=snapshot.revision_id
join mods mod on mod.id=revision.mod_id
join mod_content_versions version on version.id=revision.target_version_id
where snapshot.revision_id='{revision}';
commit;
""")
resources = json.loads(raw)
assert len(resources) == 3
(output / "canonical-database.json").write_text(json.dumps(resources, indent=2) + "\n")

expected_kinds = {"item": "minecraft.item", "fluid": "minecraft.fluid", "mekanism_gas": "mekanism.gas"}
checks = []
for case in runtime["gr010"]["cases"]:
    resource = next(row for row in resources if row["kind"] == expected_kinds[case["kind"]])
    assert resource["active"] and resource["revisionId"] == revision
    assert resource["publicId"] and resource["versionPublicId"] and resource["modSiteId"]
    with urllib.request.urlopen(runtime["apiOrigin"] + "/api/v1/catalog/recipes/" + case["recipePublicId"] + "/render", timeout=15) as response:
        body = response.read()
    data = json.loads(body)["data"]
    slot = next(slot for slot in data["layout"]["slots"] if slot["role"] == "output")
    assert len(slot["alternatives"]) == 1
    reference = slot["alternatives"][0]
    for key in ("publicId", "kind", "iconPath"):
        assert reference[key] == resource[key], (case["kind"], key)
    assert reference["id"] == resource["canonicalId"]
    assert reference["sourceRevisionId"] == resource["revisionId"]
    assert reference["sourceVersionPublicId"] == resource["versionPublicId"]
    assert reference["sourceModSiteId"] == resource["modSiteId"]
    assert reference["amount"] == case["amount"]
    expected_icon = next(asset["path"] for asset in runtime["renderAssets"] if asset["purpose"] == case["kind"])
    assert reference["iconPath"] == expected_icon
    assert reference["detailUrl"] == "/mods/" + resource["modSiteId"] + "/resources/" + resource["publicId"] + "?version=" + resource["versionPublicId"]
    (output / (case["kind"] + "-render.json")).write_bytes(body)
    checks.append({"kind": case["kind"], "canonicalKind": resource["kind"], "publicId": resource["publicId"], "recipePublicId": case["recipePublicId"], "iconPath": expected_icon, "renderSHA256": hashlib.sha256(body).hexdigest()})
assert len({check["publicId"] for check in checks}) == 3

assets = []
for expected in runtime["renderAssets"]:
    path = "/api/v1/export-revisions/" + revision + "/assets/content?path=" + urllib.parse.quote(expected["path"], safe="")
    # Follow the normal asset redirect. Do not persist its temporary location.
    with urllib.request.urlopen(runtime["apiOrigin"] + path, timeout=15) as response:
        assert response.status == 200
        body = response.read()
    assert body[:8] == b"\x89PNG\r\n\x1a\n"
    offset = 8
    while offset < len(body):
        size = int.from_bytes(body[offset:offset + 4], "big")
        assert body[offset + 4:offset + 8] not in (b"tEXt", b"iTXt", b"zTXt")
        offset += size + 12
    assert offset == len(body)
    image = Image.open(io.BytesIO(body)).convert("RGBA")
    assert image.size == (expected["width"], expected["height"])
    assert image.tobytes() == bytes(expected["rgba"]) * image.width * image.height
    (output / (expected["purpose"] + ".png")).write_bytes(body)
    assets.append({"path": expected["path"], "purpose": expected["purpose"], "sha256": hashlib.sha256(body).hexdigest(), "rgba": expected["rgba"], "width": image.width, "height": image.height, "status": 200})

result = {"status": "PASS_READ_ONLY_CANONICAL_REFERENCES_AND_FOUR_SANITIZED_PNGS", "recordedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(), "sampleSHA256": runtime["sampleSHA256"], "sourceRevisions": runtime["repoRevisions"], "driverSHA256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(), "checks": checks, "assets": assets, "productionExecuted": False, "limits": "Actual task-owned HTTP/PostgreSQL and synthetic v7 contract. Native painting/interaction, actual NATS and real game are separate evidence."}
(output / "canonical-acceptance.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({"status": result["status"], "checks": len(checks), "assets": len(assets), "output": str(output)}))
