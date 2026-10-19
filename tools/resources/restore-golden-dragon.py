#!/usr/bin/env python3
"""Restore the four CN golden-dragon encounters using the installed Saint Dragon model."""
import argparse
import copy
import csv
import datetime
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import re
import shutil


SPEC = importlib.util.spec_from_file_location(
    "resource_patch", Path(__file__).with_name("hide-settings-github.py"))
PATCH = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PATCH)
NAME = "金闪闪龙"
GROUP = 700301102
DONOR = 31960111
MODEL = "emt_00104"
# Native parties are numbered sequentially; public encounter IDs use difficulty slots.
ENCOUNTERS = ((30110202, 30110201, "中级", 21),
              (30110203, 30110202, "上级", 31),
              (30110205, 30110203, "超级", 51),
              (30110206, 30110204, "超弩级", 61))
ENEMY_IDS = {30110200 + suffix + part for _, _, _, suffix in ENCOUNTERS
             for part in range(3) if not (suffix == 21 and part == 2)}


def csv_rows(data):
    return {int(r[0]): r for r in csv.reader(io.StringIO(data.decode("utf-8-sig")))
            if r and r[0].isdigit()}


def patch_enemy(data):
    text = data.decode("utf-8")
    seen = set()
    def replace(match):
        line = match.group(0)
        row = next(csv.reader([line]))
        if int(row[0]) not in ENEMY_IDS:
            return line
        if row[2] not in ("emt_00134", MODEL) or not row[6].startswith(NAME):
            raise ValueError("Unexpected golden-dragon identity: " + row[0])
        seen.add(int(row[0]))
        # Preserve every other CSV field, whitespace and line ending.
        return re.sub(r"^(\d+,[^,]*,)" + re.escape(row[2]) + r"(?=,)",
                      lambda m: m.group(1) + MODEL, line)
    result = re.sub(r"(?m)^\d+,[^\r\n]*", replace, text).encode("utf-8")
    if seen != ENEMY_IDS:
        raise ValueError("Golden-dragon enemy rows are incomplete")
    return result


def rewrite_container(content):
    import UnityPy
    plain = content if content.startswith(b"Unity") else PATCH.transform(content)
    env = UnityPy.load(plain)
    objects = {obj.path_id: obj for obj in env.objects}
    matches = [obj for obj in env.objects if obj.type.name == "TextAsset"
               and obj.read().m_Name == "enemy.csv"]
    if len(matches) != 1:
        raise ValueError("Expected one client enemy.csv TextAsset")
    target = matches[0]
    raw_before = {pid: obj.get_raw_data() for pid, obj in objects.items()}
    tree = target.read_typetree()
    script = tree["m_Script"]
    source = script.encode("utf-8", "surrogateescape") if isinstance(script, str) else bytes(script)
    updated = patch_enemy(source)
    expected = copy.deepcopy(tree)
    expected["m_Script"] = updated.decode("utf-8", "surrogateescape") if isinstance(script, str) else updated
    if updated != source:
        target.save_typetree(expected)
        result = env.file.save(packer="lz4")
        if not content.startswith(b"Unity"):
            result = PATCH.transform(result, encode=True)
    else:
        result = content
    verified = UnityPy.load(result if result.startswith(b"Unity") else PATCH.transform(result))
    after = {obj.path_id: obj for obj in verified.objects}
    if after.keys() != objects.keys() or after[target.path_id].read_typetree() != expected:
        raise ValueError("Client enemy table verification failed")
    if any(after[pid].get_raw_data() != raw for pid, raw in raw_before.items()
           if pid != target.path_id):
        raise ValueError("Unrelated client master objects changed")
    cabs = [key.lower() for key in verified.file.files if key.lower().startswith("cab-")]
    if len(cabs) != 1:
        raise ValueError("Expected one serialized CAB")
    return result, dict(cab_name=cabs[0], scrambled=not result.startswith(b"Unity"),
                        changed_enemy_ids=sorted(ENEMY_IDS),
                        other_objects_unchanged=True, other_object_count=len(objects)-1)


def reward(kind, count, identity=0, card=False):
    return dict(type=kind, num=count, reward_typeid=identity, card_lv=1 if card else 0,
                card_fame=1 if card else 0, card_love=0, card_skill_lv=[1] if card else [])


def restore_runtime(master, parties, enemies, levels, cards, card_templates):
    groups = {g["0"]: g for g in master["groups"]}
    donor_group = next(g for g in master["groups"] if any(b["0"] == DONOR for b in g["10"]))
    donor_boss = next(b for b in donor_group["10"] if b["0"] == DONOR)
    donor_replay = next(r for r in master["replays"] if r["boss_id"] == DONOR)
    donor_party = parties[donor_replay["enemy_party_id"]]
    if any(enemies[int(donor_party[i])][2] != MODEL for i in (2, 5, 8)):
        raise ValueError("Saint Dragon donor model changed")
    mappings = []
    for boss, party_id, difficulty, suffix in ENCOUNTERS:
        party = parties[party_id]
        ids = [int(party[i]) for i in (2, 5, 8, 11) if party[i]]
        expected = [30110200+suffix+i for i in range(2 if suffix == 21 else 3)]
        if ids != expected or any(eid not in levels for eid in ids):
            raise ValueError("Golden-dragon party or AI is incomplete")
        mappings.append(dict(boss_id=boss, enemy_party_id=party_id,
                             difficulty=difficulty, enemy_ids=ids))
    if GROUP in groups:
        existing = groups[GROUP]
        replays = {r["boss_id"]: r for r in master["replays"]}
        if existing["4"] != NAME or {b["0"] for b in existing["10"]} != {m["boss_id"] for m in mappings}:
            raise ValueError("Golden-dragon group already exists with different contents")
        if any(replays[m["boss_id"]]["enemy_party_id"] != m["enemy_party_id"] for m in mappings):
            raise ValueError("Existing golden-dragon replay differs")
        return mappings
    if any(r["boss_id"] in {m["boss_id"] for m in mappings} for r in master["replays"]):
        raise ValueError("Golden-dragon Boss ID collision")
    # Follow the installed catalog's deterministic same-element fallback policy.
    templates = {c["card_id"]: c for c in card_templates}
    eligible = sorted(cid for cid, c in cards.items() if len(c) > 94 and c[94] == "活动副本"
                      and cid in templates and templates[cid]["fusion_attributes"] == 8)
    if not eligible:
        raise ValueError("No LIGHT activity-drop card is available")
    index = int.from_bytes(hashlib.sha256((NAME+"\0"+"60000154").encode()).digest(), "big") % len(eligible)
    card_id = eligible[index]
    profile = master["local_profile"]
    policy = profile["repeatable_reward_policy"]
    overrides = {r["display_name"]: r for r in policy["difficulty_overrides"]["entries"]}
    audit = profile["drop_catalog_summary"]
    source = "LOCAL_POLICY_OFFICIAL_ACTIVITY_DUNGEON_ELEMENT_MATCH"
    group = copy.deepcopy(donor_group)
    group.update({"0": GROUP, "4": NAME, "10": []})
    for mapping in mappings:
        boss_id, difficulty = mapping["boss_id"], mapping["difficulty"]
        tier = policy["tiers"][min(8, boss_id % 100)-1]
        economy = overrides[difficulty]
        boss = copy.deepcopy(donor_boss)
        boss.update({"0": boss_id, "4": difficulty, "5": economy["bp_use"],
                     "6": economy["bp_use_half"], "11": NAME,
                     "12": [{"0": card_id, "1": 0}], "7": 0})
        group["10"].append(boss)
        replay = copy.deepcopy(donor_replay)
        replay.update(boss_id=boss_id, enemy_party_id=mapping["enemy_party_id"], seed=boss_id,
                      battles=[dict(enemy_party_id=mapping["enemy_party_id"], enemy_type=1)])
        master["replays"].append(replay)
        rewards = [reward(0, economy["player_experience"]), reward(4, tier["gold"]),
                   reward(13, 1, tier["stack_card_id"]), reward(6, 1, card_id, card=True),
                   reward(8, 100*tier["tier"], 4000)]
        drops = [dict(battle_index=0, enemy_index=0, reward=copy.deepcopy(r)) for r in rewards[1:]]
        for slot in range(1, len(mapping["enemy_ids"])):
            drops.append(dict(battle_index=0, enemy_index=slot, reward=reward(13, 1, tier["stack_card_id"])))
        master["rewards"].append(dict(boss_id=boss_id, stage_quest_area_id=0,
            stage_quest_stage_id=0, result_rewards=rewards,
            first_clear_rewards=[reward(10, 50)], enemy_drops=drops))
        audit["assignment_audit"].append(dict(boss_id=boss_id, primary_name=NAME,
            primary_picture_id=60000154, card_id=card_id, card_name=templates[card_id]["name"], source=source))
        audit["assignment_source_counts"][source] += 1
        summary = master["summary"]
        summary["repeatable_reward_tier_counts"][str(tier["tier"])] += 1
        summary["difficulty_override_counts"][economy["difficulty_key"]] += 1
    master["groups"].append(group)
    master["groups"].sort(key=lambda g: g["0"])
    for key in ("replays", "rewards"):
        master[key].sort(key=lambda r: r["boss_id"])
    master["recommendations"].append(dict(bossid=30110206, name=NAME+" / LIGHT", attr=4, recommendid=[0]*4))
    summary = master["summary"]
    for key, amount in (("local_groups", 1), ("published_candidate_bosses", 4),
                        ("combined_unique_bosses", 4), ("fallback_activity_drop_bosses", 4),
                        ("recommendation_entries", 1)):
        summary[key] += amount
    profile["golden_dragon_restoration"] = dict(
        encounters=mappings, model_source_boss_id=DONOR, original_model="emt_00134",
        model=MODEL, appearance="Existing 日服·セイントドラゴン model; not original golden-dragon artwork",
        portrait_source_boss_id=DONOR, rewards="Editable local catalog policy; official drop values not recovered",
        scope="Original CN stats, parties, AI and skills; explicit public-ID to party-ID mapping")
    master["generated_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    return mappings


def prepare(root):
    PATCH.BUNDLE = "main_c/container.dat"
    PATCH.rewrite_bundle = rewrite_container
    originals, outputs, verification = PATCH.prepare(root)
    asset_map = json.loads(originals[json.loads(originals["resource-set.json"])["entrypoints"]["cn-asset-map"]])
    model_assets = [r for r in asset_map["assets"] if r.get("name") == MODEL+"_pack"]
    if len(model_assets) != 1 or not PATCH.inside(root, "resources/patch/"+model_assets[0]["bundle"]).is_file():
        raise ValueError("Saint Dragon model bundle is missing")
    if not PATCH.inside(root, "resources/patch/main_c/chr/chr_"+MODEL+"_txt.dat").is_file():
        raise ValueError("Saint Dragon animation text bundle is missing")
    manifest = json.loads(outputs["resource-set.json"])
    inventory = {r["path"]: r for r in manifest["files"]}
    runtime_name = manifest["entrypoints"]["cn-battle-master"]
    combat = manifest["entrypoints"]["cn-combat-master-root"]
    enemy_name = combat+"/enemy.csv"
    def read(name):
        data = PATCH.inside(root, name).read_bytes()
        row = inventory[name]
        if len(data) != row["bytes"] or PATCH.digest(data) != row["sha256"]:
            raise ValueError("Resource inventory mismatch: " + name)
        originals[name] = data
        return data
    runtime = json.loads(read(runtime_name))
    enemy_data = read(enemy_name)
    parties = csv_rows(read(combat+"/enemy_party.csv"))
    levels = csv_rows(read(combat+"/enemy_lvup.csv"))
    cards = csv_rows(read(manifest["entrypoints"]["cn-combat-card-master"]))
    card_master = json.loads(read(manifest["entrypoints"]["cn-card-master"]))
    mappings = restore_runtime(runtime, parties, csv_rows(enemy_data), levels,
                               cards, card_master["card_templates"])
    outputs[runtime_name] = PATCH.serialize(runtime)
    outputs[enemy_name] = patch_enemy(enemy_data)
    for name in (runtime_name, enemy_name):
        data = outputs[name]
        row = inventory[name]
        manifest["summary"]["bytes"] += len(data)-row["bytes"]
        row.update(bytes=len(data), sha256=PATCH.digest(data))
    outputs["resource-set.json"] = PATCH.serialize(manifest)
    # Publish the inventory last, after its content.
    outputs["resource-set.json"] = outputs.pop("resource-set.json")
    verification.update(change="Restore CN golden-dragon catalog and Saint Dragon appearance",
        model_source_boss_id=DONOR, model=MODEL, original_model_available=False,
        encounters=mappings, official_golden_dragon_artwork_claimed=False,
        other_enemy_fields_unchanged=True, native_ai_and_skills_unchanged=True,
        source_files=[dict(path=n, bytes=len(b), sha256=PATCH.digest(b)) for n,b in originals.items()],
        files=[dict(path=n, bytes=len(b), sha256=PATCH.digest(b)) for n,b in outputs.items()])
    return originals, outputs, verification


def apply(root, originals, outputs, verification):
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    backup = root.parent/"golden-dragon-backups"/stamp
    for name, content in originals.items():
        if PATCH.inside(root, name).read_bytes() != content:
            raise ValueError("Resource changed during preparation: " + name)
    backup.mkdir(parents=True)
    for name in outputs:
        target = backup/name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(PATCH.inside(root, name), target)
    replaced = []
    try:
        for name, content in outputs.items():
            target = PATCH.inside(root, name)
            if target.read_bytes() != originals[name]:
                raise ValueError("Resource changed during installation: " + name)
            PATCH.atomic_write(target, content)
            replaced.append(name)
        for name, content in outputs.items():
            if PATCH.inside(root, name).read_bytes() != content:
                raise ValueError("Installed resource differs: " + name)
    except BaseException:
        for name in reversed(replaced):
            PATCH.atomic_write(PATCH.inside(root, name), originals[name])
        raise
    (backup/"validation.json").write_bytes(PATCH.serialize(verification))
    return backup


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("resource_set", type=Path)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--output", type=Path)
    mode.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    try:
        root = args.resource_set.resolve(strict=True)
        originals, outputs, verification = prepare(root)
        if args.output:
            PATCH.export(root, args.output, outputs, verification)
        elif args.apply:
            print("Backup:", apply(root, originals, outputs, verification))
        print(json.dumps(verification, ensure_ascii=False, indent=2))
    except (ValueError, OSError, KeyError, StopIteration, ImportError) as exc:
        parser.exit(1, "ERROR: " + str(exc) + "\n")


if __name__ == "__main__":
    main()
