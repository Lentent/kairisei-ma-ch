import contextlib
import hashlib
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import unittest
import uuid


KEEP = [10000000, *range(10100001, 10100009)]
SPECIAL = 26093002
UNOWNED_SPECIAL = 26093003
MIXED_SPECIAL = 26093004
UNKNOWN = 77777777
TABLES = [('cn_save_snapshot', 'singleton'), ('cn_account_snapshot', 'user_id')]


@contextlib.contextmanager
def test_directory():
    root = Path(__file__).resolve().parent / ('honor-test-' + uuid.uuid4().hex)
    root.mkdir()
    try:
        yield root
    finally:
        shutil.rmtree(root)


class ResetTest(unittest.TestCase):
    def create_fixture(self, root):
        dbpath, seed, master = [root / name for name in ('save.sqlite3', 'seed.json', 'master.json')]
        state = {
            'honors': {
                'honorids': [10000000, 999, SPECIAL, MIXED_SPECIAL, UNKNOWN],
                'deck_honorids': [999, SPECIAL, MIXED_SPECIAL, UNKNOWN],
            },
            'other': {'gold': 123, 'progress': [1, 2]},
        }
        raw = json.dumps(state).encode()
        seed.write_bytes(raw)
        rows = [{'honor_id': n, 'slot_mask': 15 if n == 10000000 else 5,
                 'default_owned': True} for n in KEEP]
        rows.extend([
            {'honor_id': 999, 'slot_mask': 5, 'default_owned': True},
            {'honor_id': 10500001, 'slot_mask': 5, 'default_owned': True},
            {'honor_id': SPECIAL, 'slot_mask': 8, 'default_owned': True},
            {'honor_id': UNOWNED_SPECIAL, 'slot_mask': 8, 'default_owned': True},
            {'honor_id': MIXED_SPECIAL, 'slot_mask': 9, 'default_owned': True},
        ])
        master_raw = json.dumps({'honors': rows}).encode()
        master.write_bytes(master_raw)
        with contextlib.closing(sqlite3.connect(dbpath)) as db, db:
            for table, key in TABLES:
                db.execute(f'CREATE TABLE {table} ({key} INTEGER PRIMARY KEY, payload_json BLOB, payload_sha256 TEXT, revision INTEGER, updated_utc TEXT)')
                db.execute(f'INSERT INTO {table} VALUES (1, ?, ?, 2, ?)',
                           (raw, hashlib.sha256(raw).hexdigest(), 'old'))
        command = [sys.executable, str(Path(__file__).with_name('reset-honors.py')),
                   '--db', str(dbpath), '--seed', str(seed), '--master', str(master)]
        return dbpath, seed, master, state, raw, master_raw, command

    def snapshots(self, dbpath):
        with contextlib.closing(sqlite3.connect(dbpath)) as db:
            return [db.execute(f'SELECT payload_json,payload_sha256,revision,updated_utc FROM {table}').fetchone()
                    for table, _ in TABLES]

    def assert_cleaned_state(self, result, original):
        owned = result['honors']['honorids']
        self.assertEqual(set(owned), set(KEEP + [SPECIAL, MIXED_SPECIAL, UNKNOWN]))
        self.assertEqual(len(owned), len(set(owned)))
        self.assertNotIn(UNOWNED_SPECIAL, owned)
        self.assertEqual(result['honors']['deck_honorids'], [0, SPECIAL, MIXED_SPECIAL, UNKNOWN])
        self.assertEqual({k: v for k, v in result.items() if k != 'honors'},
                         {k: v for k, v in original.items() if k != 'honors'})

    def test_preview_apply_backup_preserves_special_and_unknown_honors(self):
        with test_directory() as root:
            dbpath, seed, master, state, raw, master_raw, command = self.create_fixture(root)
            before = self.snapshots(dbpath)
            subprocess.run(command, check=True, capture_output=True, timeout=20)
            self.assertEqual(seed.read_bytes(), raw)
            self.assertEqual(master.read_bytes(), master_raw)
            self.assertEqual(self.snapshots(dbpath), before)
            self.assertEqual(list(root.glob('honor-backup-*')), [])

            subprocess.run(command + ['--apply'], check=True, capture_output=True, timeout=20)
            first = self.snapshots(dbpath)
            for content, digest, revision, updated in first:
                self.assert_cleaned_state(json.loads(content), state)
                self.assertEqual(hashlib.sha256(content).hexdigest(), digest)
                self.assertEqual(revision, 3)
                self.assertNotEqual(updated, 'old')
            self.assert_cleaned_state(json.loads(seed.read_bytes()), state)
            backup = next(root.glob('honor-backup-*'))
            self.assertEqual(self.snapshots(backup / dbpath.name), before)
            self.assertEqual((backup / 'config-0.json').read_bytes(), master_raw)
            self.assertEqual((backup / 'config-1.json').read_bytes(), raw)

            defaults = {row['honor_id']: row['default_owned']
                        for row in json.loads(master.read_bytes())['honors']}
            self.assertTrue(all(defaults[n] for n in KEEP))
            self.assertFalse(defaults[999])
            self.assertFalse(defaults[10500001])
            self.assertTrue(defaults[SPECIAL])
            self.assertTrue(defaults[MIXED_SPECIAL])
            self.assertTrue(defaults[UNOWNED_SPECIAL])

            first_seed = json.loads(seed.read_bytes())
            first_master = json.loads(master.read_bytes())
            subprocess.run(command + ['--apply'], check=True, capture_output=True, timeout=20)
            for current, previous in zip(self.snapshots(dbpath), first):
                self.assertEqual(json.loads(current[0]), json.loads(previous[0]))
                self.assertEqual(hashlib.sha256(current[0]).hexdigest(), current[1])
            self.assertEqual(json.loads(seed.read_bytes()), first_seed)
            self.assertEqual(json.loads(master.read_bytes()), first_master)

    def test_invalid_master_does_not_modify_database_or_configs(self):
        for invalid_mask in [0, 16, '8']:
            with self.subTest(slot_mask=invalid_mask), test_directory() as root:
                dbpath, seed, master, _, raw, _, command = self.create_fixture(root)
                invalid = json.loads(master.read_bytes())
                invalid['honors'][-1]['slot_mask'] = invalid_mask
                invalid_raw = json.dumps(invalid).encode()
                master.write_bytes(invalid_raw)
                before = self.snapshots(dbpath)
                failed = subprocess.run(command + ['--apply'], capture_output=True, timeout=20)
                self.assertNotEqual(failed.returncode, 0)
                self.assertEqual(self.snapshots(dbpath), before)
                self.assertEqual(seed.read_bytes(), raw)
                self.assertEqual(master.read_bytes(), invalid_raw)
                self.assertEqual(list(root.glob('honor-backup-*')), [])
        with test_directory() as root:
            dbpath, seed, master, _, raw, _, command = self.create_fixture(root)
            master.write_bytes(b'{"honors": []}')
            before = self.snapshots(dbpath)
            failed = subprocess.run(command + ['--apply'], capture_output=True, timeout=20)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(self.snapshots(dbpath), before)
            self.assertEqual(seed.read_bytes(), raw)
            self.assertEqual(master.read_bytes(), b'{"honors": []}')

    def test_checksum_failure_rolls_back_all_accounts_and_configs(self):
        with test_directory() as root:
            dbpath, seed, master, _, raw, master_raw, command = self.create_fixture(root)
            with contextlib.closing(sqlite3.connect(dbpath)) as db, db:
                db.execute("UPDATE cn_account_snapshot SET payload_sha256='invalid'")
            before = self.snapshots(dbpath)
            failed = subprocess.run(command + ['--apply'], capture_output=True, timeout=20)
            self.assertNotEqual(failed.returncode, 0)
            self.assertIn(b'Checksum mismatch', failed.stderr)
            self.assertEqual(self.snapshots(dbpath), before)
            self.assertEqual(seed.read_bytes(), raw)
            self.assertEqual(master.read_bytes(), master_raw)
            self.assertEqual(list(root.glob('honor-backup-*')), [])


if __name__ == '__main__':
    unittest.main()
