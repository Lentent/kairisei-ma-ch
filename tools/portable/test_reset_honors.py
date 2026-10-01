import hashlib
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import unittest
import contextlib
import shutil
import uuid


@contextlib.contextmanager
def test_directory():
    root = Path(__file__).resolve().parent / ('honor-test-' + uuid.uuid4().hex)
    root.mkdir()
    try:
        yield root
    finally:
        shutil.rmtree(root)


class ResetTest(unittest.TestCase):
    def test_preview_apply_backup_and_invalid_master(self):
        with test_directory() as directory:
            root = Path(directory)
            dbpath, seed, master = [root / name for name in ('save.sqlite3', 'seed.json', 'master.json')]
            state = {'honors': {'honorids': [10000000, 999], 'deck_honorids': [999, 10000000, 0, 999]}, 'other': {'gold': 123}}
            raw = json.dumps(state).encode()
            seed.write_bytes(raw)
            master.write_text(json.dumps({'honors': [{'honor_id': n, 'default_owned': True} for n in [10000000, *range(10100001, 10100009), 999]]}))
            with contextlib.closing(sqlite3.connect(dbpath)) as db, db:
                for table, key in [('cn_save_snapshot', 'singleton'), ('cn_account_snapshot', 'user_id')]:
                    db.execute(f'CREATE TABLE {table} ({key} INTEGER PRIMARY KEY, payload_json BLOB, payload_sha256 TEXT, revision INTEGER, updated_utc TEXT)')
                    db.execute(f'INSERT INTO {table} VALUES (1, ?, ?, 2, ?)', (raw, hashlib.sha256(raw).hexdigest(), 'old'))
            command = [sys.executable, str(Path(__file__).with_name('reset-honors.py')), '--db', str(dbpath), '--seed', str(seed), '--master', str(master)]
            subprocess.run(command, check=True, capture_output=True, timeout=20)
            self.assertEqual(seed.read_bytes(), raw)
            with contextlib.closing(sqlite3.connect(dbpath)) as db, db:
                self.assertEqual(db.execute('SELECT revision FROM cn_save_snapshot').fetchone()[0], 2)
            subprocess.run(command + ['--apply'], check=True, capture_output=True, timeout=20)
            with contextlib.closing(sqlite3.connect(dbpath)) as db, db:
                for table in ('cn_save_snapshot', 'cn_account_snapshot'):
                    content, digest, revision = db.execute(f'SELECT payload_json,payload_sha256,revision FROM {table}').fetchone()
                    result = json.loads(content)
                    self.assertEqual(result['honors']['honorids'], [10000000, *range(10100001, 10100009)])
                    self.assertEqual(result['honors']['deck_honorids'], [0, 10000000, 0, 0])
                    self.assertEqual(result['other'], state['other'])
                    self.assertEqual(hashlib.sha256(content).hexdigest(), digest)
                    self.assertEqual(revision, 3)
            backup = next(root.glob('honor-backup-*'))
            with contextlib.closing(sqlite3.connect(backup / dbpath.name)) as db, db:
                self.assertEqual(db.execute('SELECT payload_json FROM cn_save_snapshot').fetchone()[0], raw)
            self.assertFalse(json.loads(master.read_text())['honors'][-1]['default_owned'])
            master.write_text('{"honors": []}')
            failed = subprocess.run(command + ['--apply'], capture_output=True, timeout=20)
            self.assertNotEqual(failed.returncode, 0)
            with contextlib.closing(sqlite3.connect(dbpath)) as db, db:
                self.assertEqual(db.execute('SELECT revision FROM cn_save_snapshot').fetchone()[0], 3)


if __name__ == '__main__':
    unittest.main()
