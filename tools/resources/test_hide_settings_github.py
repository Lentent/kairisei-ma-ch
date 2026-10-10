import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch
import uuid
import zlib


spec = importlib.util.spec_from_file_location("settings_patch", Path(__file__).with_name("hide-settings-github.py"))
tool = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tool)


class InstallerTests(unittest.TestCase):
    def setUp(self):
        temp_parent = Path(os.environ.get("SETTINGS_PATCH_TEST_TMP", tempfile.gettempdir())).resolve()
        test_dir = temp_parent / ("settings-github-test-" + uuid.uuid4().hex)
        test_dir.mkdir(mode=0o777)

        def cleanup():
            if test_dir.resolve().parent != temp_parent or not test_dir.name.startswith("settings-github-test-"):
                raise ValueError("Unexpected test cleanup path")
            shutil.rmtree(test_dir)

        self.addCleanup(cleanup)
        self.root = test_dir / "resource-set"
        self.root.mkdir()
        self.bundle_name = "patch/" + tool.BUNDLE
        self.old_bundle = b"scrambled source fixture"
        self.new_bundle = b"UnityFS replacement fixture"
        crc = "%08X" % (zlib.crc32(self.old_bundle) & 0xffffffff)
        self.version_plain = ("<version>,790\r\n<bundle_ver>," + tool.BUNDLE + ",0," + crc +
                              "\r\n<bundle_ver>,other.dat,0,12345678\r\n").encode()
        self.asset_map = {
            "schema_version": 2, "client_profile": "cn602-bootstrap",
            "source": {"version_dat_sha256": tool.digest(tool.transform(self.version_plain, True)),
                       "scrambled_bundle_count": 2, "plain_bundle_count": 0,
                       "version_manifests": [{"patch_root": "patch", "sha256": tool.digest(tool.transform(self.version_plain, True))}]},
            "bundles": [{"bundle": tool.BUNDLE, "cab_name": "cab-fixture", "scrambled": True, "delivery_crc32": crc},
                        {"bundle": "other.dat", "cab_name": "cab-other", "scrambled": True, "custom": "retain"}],
            "catalog_assets": [{"name": "custom skin", "bundle": "other.dat"}],
            "custom_operator_metadata": {"keep": [1, 2, 3]},
        }
        contents = {self.bundle_name: self.old_bundle,
                    "patch/version.dat": tool.transform(self.version_plain, True),
                    "asset-map.json": tool.serialize(self.asset_map), "custom.csv": b"custom content"}
        manifest = {"schema_version": 1, "client_profile": "cn602-bootstrap", "path_base": "RESOURCE_SET_ROOT",
                    "entrypoints": {"cn-patch-root": "patch", "cn-asset-map": "asset-map.json"},
                    "files": [{"path": name, "bytes": len(data), "sha256": tool.digest(data)} for name, data in contents.items()],
                    "summary": {"file_count": len(contents), "bytes": sum(map(len, contents.values()))},
                    "custom": "retain"}
        contents["resource-set.json"] = tool.serialize(manifest)
        for name, data in contents.items():
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        self.contents = contents
        self.verification = {"cab_name": "cab-fixture", "scrambled": False, "text_after": ""}
        mock = patch.object(tool, "rewrite_bundle", return_value=(self.new_bundle, copy.deepcopy(self.verification)))
        mock.start()
        self.addCleanup(mock.stop)

    def test_merge_preserves_custom_metadata_and_versions(self):
        originals, outputs, report = tool.prepare(self.root)
        # Preparation/check mode leaves every installed byte unchanged.
        self.assertEqual({name: (self.root / name).read_bytes() for name in self.contents}, self.contents)
        asset_map = json.loads(outputs["asset-map.json"])
        self.assertEqual(asset_map["bundles"][1], self.asset_map["bundles"][1])
        self.assertEqual(asset_map["catalog_assets"], self.asset_map["catalog_assets"])
        self.assertEqual(asset_map["custom_operator_metadata"], self.asset_map["custom_operator_metadata"])
        self.assertEqual(asset_map["source"]["scrambled_bundle_count"], 1)
        self.assertEqual(asset_map["source"]["plain_bundle_count"], 1)
        new_plain = tool.transform(outputs["patch/version.dat"])
        self.assertIn(b"<version>,790\r\n", new_plain)
        self.assertIn(b"<bundle_ver>,other.dat,0,12345678\r\n", new_plain)
        self.assertIn(report["crc32"].encode(), new_plain)
        manifest = json.loads(outputs["resource-set.json"])
        self.assertEqual(manifest["custom"], "retain")
        self.assertEqual(manifest["summary"]["bytes"], sum(row["bytes"] for row in manifest["files"]))
        for row in manifest["files"]:
            data = outputs.get(row["path"], self.contents.get(row["path"]))
            self.assertEqual(row["sha256"], tool.digest(data))
        backup = tool.apply(self.root, originals, outputs, report)
        for name, data in originals.items():
            self.assertEqual((backup / name).read_bytes(), data)
            self.assertEqual((self.root / name).read_bytes(), outputs[name])
        self.assertEqual((self.root / "custom.csv").read_bytes(), b"custom content")

    def test_invalid_hash_refuses_before_modification(self):
        (self.root / self.bundle_name).write_bytes(b"tampered")
        with self.assertRaisesRegex(ValueError, "hash/size mismatch"):
            tool.prepare(self.root)
        self.assertFalse((self.root.parent / "settings-github-backups").exists())

    def test_four_server_files_are_enough_for_offline_export(self):
        # The copied server manifest still lists every resource, even though
        # this offline input contains only the four files being changed.
        (self.root / "custom.csv").unlink()
        original_manifest = json.loads((self.root / "resource-set.json").read_bytes())
        _, outputs, report = tool.prepare(self.root)
        destination = self.root.parent / "export"
        tool.export(self.root, destination, outputs, report)
        exported = json.loads((destination / "resource-set/resource-set.json").read_bytes())
        before = next(row for row in original_manifest["files"] if row["path"] == "custom.csv")
        after = next(row for row in exported["files"] if row["path"] == "custom.csv")
        self.assertEqual(before, after)
        self.assertEqual(len(list((destination / "resource-set").rglob("*.*"))), 4)

    def test_server_layout_and_custom_indexes_can_differ(self):
        # These are the server's paths, not the paths from a local source bundle.
        new_patch = "server-current/patches"
        new_map = "metadata/server-asset-map.json"
        new_bundle = new_patch + "/" + tool.BUNDLE
        (self.root / "server-current").mkdir()
        (self.root / "patch").rename(self.root / new_patch)
        (self.root / "metadata").mkdir()
        (self.root / "asset-map.json").rename(self.root / new_map)
        manifest = json.loads((self.root / "resource-set.json").read_bytes())
        manifest["entrypoints"].update({"cn-patch-root": new_patch, "cn-asset-map": new_map})
        remapped = {}
        for row in manifest["files"]:
            name = row["path"]
            if name.startswith("patch/"):
                row["path"] = new_patch + name[len("patch"):]
            elif name == "asset-map.json":
                row["path"] = new_map
            remapped[row["path"]] = row
        server_map = copy.deepcopy(self.asset_map)
        server_map["source"]["version_manifests"][0]["patch_root"] = new_patch
        server_map["server_only"] = {"costume_version": 25}
        server_map_bytes = tool.serialize(server_map)
        (self.root / new_map).write_bytes(server_map_bytes)
        remapped[new_map].update(bytes=len(server_map_bytes), sha256=tool.digest(server_map_bytes))
        custom = b"server-specific custom card configuration"
        (self.root / "custom.csv").write_bytes(custom)
        remapped["custom.csv"].update(bytes=len(custom), sha256=tool.digest(custom))
        manifest["summary"]["bytes"] = sum(row["bytes"] for row in manifest["files"])
        (self.root / "resource-set.json").write_bytes(tool.serialize(manifest))
        originals, outputs, report = tool.prepare(self.root)
        self.assertEqual(set(outputs), {"resource-set.json", new_map, new_bundle, new_patch + "/version.dat"})
        patched_map = json.loads(outputs[new_map])
        self.assertEqual(patched_map["server_only"], server_map["server_only"])
        self.assertEqual(patched_map["bundles"][1], server_map["bundles"][1])
        self.assertEqual(patched_map["source"]["version_manifests"][0]["sha256"], tool.digest(outputs[new_patch + "/version.dat"]))
        self.assertEqual({row["path"] for row in report["source_files"]}, set(originals))
        tool.apply(self.root, originals, outputs, report)
        self.assertEqual((self.root / "custom.csv").read_bytes(), custom)

    def test_partial_write_failure_rolls_back(self):
        originals, outputs, report = tool.prepare(self.root)
        real_write = tool.atomic_write
        calls = 0

        def failing_write(path, data):
            nonlocal calls
            calls += 1
            if calls == 2:
                raise OSError("simulated disk error")
            real_write(path, data)

        with patch.object(tool, "atomic_write", side_effect=failing_write):
            with self.assertRaisesRegex(OSError, "simulated disk error"):
                tool.apply(self.root, originals, outputs, report)
        for name, data in self.contents.items():
            self.assertEqual((self.root / name).read_bytes(), data)

    def test_concurrent_change_refuses_install(self):
        originals, outputs, report = tool.prepare(self.root)
        (self.root / self.bundle_name).write_bytes(b"a newer custom bundle")
        with self.assertRaisesRegex(ValueError, "during preparation"):
            tool.apply(self.root, originals, outputs, report)
        self.assertFalse((self.root.parent / "settings-github-backups").exists())


class RealBundleTests(unittest.TestCase):
    def setUp(self):
        fixture = os.environ.get("SETTINGS_PATCH_BUNDLE_FIXTURE")
        if not fixture:
            self.skipTest("set SETTINGS_PATCH_BUNDLE_FIXTURE to a ui_option.dat fixture")
        self.original = Path(fixture).read_bytes()
        import UnityPy
        self.unity = UnityPy

    def load(self, content):
        return self.unity.load(content if content.startswith(b"Unity") else tool.transform(content))

    def customized_server_bundle(self, text):
        env = self.load(self.original)
        obj = next(obj for obj in env.objects if obj.path_id == tool.LABEL_ID)
        label = obj.read_typetree()
        label["mText"] = text
        label["mMaxLineWidth"] += 37
        obj.save_typetree(label)
        bundle = env.file.save(packer="lz4")
        self.assertNotEqual(bundle, self.original)
        return bundle

    def test_changed_server_ui_preserves_custom_fields(self):
        server_bundle = self.customized_server_bundle("项目地址：https://github.com/Lentent/kairisei-ma-ch")
        before = {obj.path_id: obj for obj in self.load(server_bundle).objects}
        patched, report = tool.rewrite_bundle(server_bundle)
        after = {obj.path_id: obj for obj in self.load(patched).objects}
        expected = before[tool.LABEL_ID].read_typetree()
        expected["mText"] = ""
        self.assertEqual(after[tool.LABEL_ID].read_typetree(), expected)
        self.assertEqual(set(before), set(after))
        for key in before:
            if key != tool.LABEL_ID:
                self.assertEqual(before[key].get_raw_data(), after[key].get_raw_data())
        self.assertTrue(report["other_objects_unchanged"])
        repeated, _ = tool.rewrite_bundle(patched)
        self.assertEqual(repeated, patched)

    def test_unrecognized_server_text_is_refused(self):
        server_bundle = self.customized_server_bundle("音量设置")
        with self.assertRaisesRegex(ValueError, "unrecognized"):
            tool.rewrite_bundle(server_bundle)


if __name__ == "__main__":
    unittest.main()
