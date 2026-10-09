#!/usr/bin/env python3
"""Clear the settings link in the installed resource set, preserving other edits."""
import argparse
import copy
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import zlib


BUNDLE = "main_c/ui/ui_option.dat"
LABEL_ID = -7815472476857301814
KEY = bytes.fromhex("01cd458967ab23ef")


def digest(data):
    return hashlib.sha256(data).hexdigest()


def transform(data, encode=False):
    sign = 1 if encode else -1
    return bytes((byte + sign * KEY[i % len(KEY)]) % 256
                 for i, byte in enumerate(data))


def serialize(value):
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def validate_name(name):
    if not isinstance(name, str) or not name or "\\" in name or ":" in name:
        raise ValueError("Invalid resource path: " + str(name))
    if name.startswith("/") or any(p in ("", ".", "..") for p in name.split("/")):
        raise ValueError("Unsafe resource path: " + name)


def inside(root, name):
    validate_name(name)
    candidate = root / name
    # No links in a path that will be read or replaced.
    for part in (candidate, *candidate.parents):
        if part == root.parent:
            break
        if part.is_symlink() or getattr(part, "is_junction", lambda: False)():
            raise ValueError("Linked resource path: " + str(part))
    candidate.resolve().relative_to(root)
    return candidate


def rewrite_bundle(content):
    try:
        import UnityPy
    except ImportError as exc:
        raise ValueError("Install dependencies first: python3 -m pip install -r requirements.txt") from exc
    decoded = content if content.startswith(b"Unity") else transform(content)
    if not decoded.startswith(b"UnityFS\0"):
        raise ValueError("Settings resource is not a supported UnityFS bundle")
    env = UnityPy.load(decoded)
    objects = {obj.path_id: obj for obj in env.objects}
    if LABEL_ID not in objects or objects[LABEL_ID].type.name != "MonoBehaviour":
        raise ValueError("Expected settings label not found; no files were changed")
    label = objects[LABEL_ID]
    before = label.read_typetree()
    old_text = before.get("mText")
    if not isinstance(old_text, str):
        raise ValueError("Settings label has no text field")
    if old_text and not re.search(r"github|ma\.16163\.com", old_text, re.I):
        raise ValueError("Settings text is unrecognized; refusing to clear a different label")
    raw_before = {key: obj.get_raw_data() for key, obj in objects.items()}
    if old_text:
        after = copy.deepcopy(before)
        after["mText"] = ""
        label.save_typetree(after)
        patched = env.file.save(packer="lz4")
    else:
        patched = content
    verified = UnityPy.load(patched if patched.startswith(b"Unity") else transform(patched))
    new_objects = {obj.path_id: obj for obj in verified.objects}
    if objects.keys() != new_objects.keys():
        raise ValueError("Unity object inventory changed")
    expected = copy.deepcopy(before)
    expected["mText"] = ""
    if new_objects[LABEL_ID].read_typetree() != expected:
        raise ValueError("Settings label changed fields other than mText")
    if any(new_objects[key].get_raw_data() != raw for key, raw in raw_before.items()
           if key != LABEL_ID):
        raise ValueError("Other Unity objects changed")
    cab_names = [name.lower() for name in verified.file.files if name.lower().startswith("cab-")]
    if len(cab_names) != 1:
        raise ValueError("Expected exactly one serialized CAB file")
    return patched, {
        "object_path_id": LABEL_ID,
        "text_before": old_text,
        "text_after": "",
        "label_other_fields_unchanged": True,
        "other_objects_unchanged": True,
        "other_object_count": len(objects) - 1,
        "bundle_reloaded": True,
        "cab_name": cab_names[0],
        "scrambled": not patched.startswith(b"Unity"),
    }


def prepare(root):
    manifest_path = inside(root, "resource-set.json")
    originals = {"resource-set.json": manifest_path.read_bytes()}
    manifest = json.loads(originals["resource-set.json"])
    if (manifest.get("schema_version"), manifest.get("client_profile"), manifest.get("path_base")) != (
            1, "cn602-bootstrap", "RESOURCE_SET_ROOT"):
        raise ValueError("Unsupported resource-set manifest")
    patch_root = manifest["entrypoints"]["cn-patch-root"]
    map_name = manifest["entrypoints"]["cn-asset-map"]
    bundle_name = patch_root + "/" + BUNDLE
    version_name = patch_root + "/version.dat"
    if len({"resource-set.json", map_name, bundle_name, version_name}) != 4:
        raise ValueError("Resource entrypoints must refer to four distinct files")
    rows = {}
    for row in manifest["files"]:
        validate_name(row["path"])
        if row["path"] in rows:
            raise ValueError("Duplicate resource inventory entry")
        rows[row["path"]] = row
    if (len(rows) != manifest["summary"]["file_count"] or
            sum(row["bytes"] for row in rows.values()) != manifest["summary"]["bytes"]):
        raise ValueError("Resource inventory summary is inconsistent")
    for name in (bundle_name, version_name, map_name):
        data = inside(root, name).read_bytes()
        if name not in rows or len(data) != rows[name]["bytes"] or digest(data) != rows[name]["sha256"]:
            raise ValueError("Resource inventory hash/size mismatch: " + name)
        originals[name] = data
    asset_map = json.loads(originals[map_name])
    if (asset_map.get("schema_version"), asset_map.get("client_profile")) != (2, "cn602-bootstrap"):
        raise ValueError("Unsupported asset map")
    if asset_map["source"]["version_dat_sha256"] != digest(originals[version_name]):
        raise ValueError("Asset map belongs to a different version.dat")
    bundles = [row for row in asset_map["bundles"] if row["bundle"] == BUNDLE]
    if len(bundles) != 1:
        raise ValueError("Settings bundle missing or duplicated in asset map")
    bundle, verification = rewrite_bundle(originals[bundle_name])
    metadata = bundles[0]
    if metadata["cab_name"].lower() != verification["cab_name"]:
        raise ValueError("Settings CAB does not match the asset map")
    old_scrambled = not originals[bundle_name].startswith(b"Unity")
    if metadata["scrambled"] != old_scrambled:
        raise ValueError("Settings storage flag does not match the resource")
    old_crc = "%08X" % (zlib.crc32(originals[bundle_name]) & 0xffffffff)
    crc = "%08X" % (zlib.crc32(bundle) & 0xffffffff)
    plain_version = transform(originals[version_name])
    # Replace only the CRC; retain all unrelated bytes, row ordering and line endings.
    pattern = rb"(?m)^(<bundle_ver>," + re.escape(BUNDLE.encode()) + rb",[^,\r\n]*,)([0-9A-Fa-f]{8})(\r?$)"
    matches = list(re.finditer(pattern, plain_version))
    if len(matches) != 1:
        raise ValueError("Expected exactly one settings entry in version.dat")
    effective_crc = metadata.get("delivery_crc32") or matches[0].group(2).decode()
    if effective_crc.upper() != old_crc:
        raise ValueError("Settings delivery CRC does not match the installed bytes")
    new_plain = re.sub(pattern, lambda m: m.group(1) + crc.encode() + m.group(3), plain_version)
    version = transform(new_plain, encode=True)
    metadata.update(cab_name=verification["cab_name"], scrambled=verification["scrambled"], delivery_crc32=crc)
    source = asset_map["source"]
    source["version_dat_sha256"] = digest(version)
    if old_scrambled != verification["scrambled"]:
        delta = 1 if verification["scrambled"] else -1
        source["scrambled_bundle_count"] += delta
        source["plain_bundle_count"] -= delta
    for row in source.get("version_manifests", []):
        if row.get("patch_root") == patch_root:
            row["sha256"] = digest(version)
    outputs = {bundle_name: bundle, version_name: version, map_name: serialize(asset_map)}
    for name, data in outputs.items():
        row = rows[name]
        manifest["summary"]["bytes"] += len(data) - row["bytes"]
        row.update(bytes=len(data), sha256=digest(data))
    outputs["resource-set.json"] = serialize(manifest)
    verification.update(
        change="Clear only settings link mText",
        resource_set=str(root),
        crc32=crc,
        old_crc32=old_crc,
        version_dat_sha256=digest(version),
        device_verified=False,
        source_files=[{"path": name, "bytes": len(data), "sha256": digest(data)}
                      for name, data in originals.items()],
        files=[{"path": name, "bytes": len(data), "sha256": digest(data)}
               for name, data in outputs.items()],
    )
    return originals, outputs, verification


def export(root, destination, outputs, verification):
    destination = destination.resolve()
    if destination == root or root in destination.parents:
        raise ValueError("Output directory must be outside resource-set")
    if destination.exists():
        raise ValueError("Output directory already exists; select a new path")
    destination.mkdir(parents=True)
    for name, data in outputs.items():
        target = destination / "resource-set" / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    (destination / "validation.json").write_bytes(serialize(verification))


def atomic_write(path, content):
    permissions = path.stat().st_mode & 0o777
    fd, name = tempfile.mkstemp(prefix=".settings-github-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(name, permissions)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def apply(root, originals, outputs, verification):
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    backup_base = root.parent / "settings-github-backups"
    if backup_base.is_symlink() or getattr(backup_base, "is_junction", lambda: False)():
        raise ValueError("Backup directory must not be linked")
    backup = backup_base / stamp
    # Check for concurrent edits again before creating backups or replacing files.
    for name, content in originals.items():
        if inside(root, name).read_bytes() != content:
            raise ValueError("Resource changed during preparation: " + name)
    backup.mkdir(parents=True)
    for name in originals:
        target = backup / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(inside(root, name), target)
    replaced = []
    try:
        for name, data in outputs.items():
            target = inside(root, name)
            if target.read_bytes() != originals[name]:
                raise ValueError("Resource changed during installation: " + name)
            atomic_write(target, data)
            replaced.append(name)
        for name, data in outputs.items():
            if inside(root, name).read_bytes() != data:
                raise ValueError("Installed resource differs: " + name)
    except BaseException:
        for name in reversed(replaced):
            atomic_write(inside(root, name), originals[name])
        raise
    (backup / "validation.json").write_bytes(serialize(verification))
    return backup


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("resource_set", type=Path, help="Installed resource-set directory")
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument("--check", action="store_true", help="Prepare and validate without writing")
    modes.add_argument("--output", type=Path, help="Export four files to a new directory")
    modes.add_argument("--apply", action="store_true", help="Back up and replace files after stopping server")
    args = parser.parse_args()
    try:
        root = args.resource_set.resolve(strict=True)
        originals, outputs, verification = prepare(root)
        if args.output:
            export(root, args.output, outputs, verification)
        elif args.apply:
            print("Backup:", apply(root, originals, outputs, verification))
        print(json.dumps(verification, ensure_ascii=False, indent=2))
    except (ValueError, OSError, KeyError) as exc:
        parser.exit(1, "ERROR: " + str(exc) + "\n")


if __name__ == "__main__":
    main()
