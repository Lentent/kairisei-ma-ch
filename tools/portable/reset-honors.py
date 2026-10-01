#!/usr/bin/env python3
"""Offline honor reset. Preview by default; stop the server before --apply."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3

KEEP = [10000000, *range(10100001, 10100009)]


def encode(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode('utf-8')


def reset(state):
    honors = state['honors']
    deck = honors['deck_honorids']
    if not isinstance(deck, list) or len(deck) != 4:
        raise ValueError('Expected exactly four honor deck slots')
    honors['honorids'] = KEEP[:]
    honors['deck_honorids'] = [value if value in KEEP else 0 for value in deck]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', required=True, type=Path)
    parser.add_argument('--seed', required=True, type=Path)
    parser.add_argument('--master', required=True, type=Path)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    args.db, args.seed, args.master = (p.resolve(strict=True) for p in (args.db, args.seed, args.master))
    if len({args.db, args.seed, args.master}) != 3:
        raise ValueError('Database, seed and master must be distinct files')
    master = json.loads(args.master.read_text(encoding='utf-8-sig'))
    ids = [row['honor_id'] for row in master['honors']]
    if len(ids) != len(set(ids)) or not set(KEEP).issubset(ids):
        raise ValueError('Honor master is missing requested IDs or contains duplicates; no changes made')
    for row in master['honors']:
        row['default_owned'] = row['honor_id'] in KEEP
    seed = json.loads(args.seed.read_text(encoding='utf-8-sig'))
    reset(seed)
    replacements = {args.master: encode(master), args.seed: encode(seed)}
    db = sqlite3.connect(args.db.as_uri() + ('?mode=rw' if args.apply else '?mode=ro'), uri=True, timeout=5)
    backup = None
    changed_files = []
    try:
        db.execute('BEGIN IMMEDIATE' if args.apply else 'BEGIN')
        if db.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
            raise ValueError('Database integrity check failed')
        updates = []
        for table, key in [('cn_save_snapshot', 'singleton'), ('cn_account_snapshot', 'user_id')]:
            for identity, raw, digest in db.execute(f'SELECT {key}, payload_json, payload_sha256 FROM {table}'):
                raw = raw.encode('utf-8') if isinstance(raw, str) else raw
                if hashlib.sha256(raw).hexdigest() != digest:
                    raise ValueError(f'Checksum mismatch: {table}/{identity}')
                state = json.loads(raw)
                reset(state)
                content = encode(state)
                updates.append((table, key, identity, content))
        print(f'Snapshots: {len(updates)}; default honors: {KEEP}')
        print(f'Database: {args.db}\nSeed: {args.seed}\nMaster: {args.master}')
        if not args.apply:
            print('Preview only. Stop the server, then repeat with --apply.')
            return
        stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
        backup = args.db.parent / ('honor-backup-' + stamp)
        backup.mkdir(mode=0o700 if os.name == 'posix' else 0o755)
        # Back up through a second connection while the write lock prevents changes.
        with sqlite3.connect(args.db.as_uri() + '?mode=ro', uri=True) as source:
            with sqlite3.connect(backup / args.db.name) as destination:
                source.backup(destination)
        for index, path in enumerate(replacements):
            shutil.copy2(path, backup / f'config-{index}.json')
        (backup / 'paths.json').write_text(json.dumps([str(p) for p in replacements]), encoding='utf-8')
        now = datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00', 'Z')
        for table, key, identity, content in updates:
            db.execute(f'UPDATE {table} SET payload_json=?, payload_sha256=?, revision=revision+1, updated_utc=? WHERE {key}=?',
                       (content, hashlib.sha256(content).hexdigest(), now, identity))
        # Existing projections become stale through the revision increment.
        # Accounts.New synchronizes them from authoritative snapshots at startup.
        for path, content in replacements.items():
            temporary = path.with_name(path.name + '.honor-reset-' + stamp)
            try:
                with temporary.open('xb') as output:
                    output.write(content)
                    output.flush()
                    os.fsync(output.fileno())
                shutil.copymode(path, temporary)
                os.replace(temporary, path)
                changed_files.append(path)
            finally:
                if temporary.exists():
                    temporary.unlink()
        db.commit()
        print(f'Done. Backup: {backup}\nRestart the server to rebuild honor projections.')
    except BaseException:
        db.rollback()
        if backup:
            for index, path in enumerate(replacements):
                if path in changed_files:
                    shutil.copy2(backup / f'config-{index}.json', path)
            print(f'Backup retained at: {backup}')
        raise
    finally:
        db.close()


if __name__ == '__main__':
    main()
