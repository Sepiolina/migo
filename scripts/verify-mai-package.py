#!/usr/bin/env python3
"""Verify a release before extraction. Obtain this verifier from a trusted source."""
import argparse
import base64
import hashlib
import pathlib
import re
import stat
import subprocess
import tempfile
import zipfile


def verify(archive, trusted_key=None, allow_unsigned=False):
    with zipfile.ZipFile(archive) as package:
        entries = package.infolist()
        if len(entries) > 1000 or sum(i.file_size for i in entries) > 256 * 1024 * 1024:
            raise ValueError('Archive exceeds verification limits')
        files = {}
        seen = set()
        for item in entries:
            path = pathlib.PurePosixPath(item.filename)
            if (item.filename in seen or '\\' in item.filename or path.is_absolute()
                    or '..' in path.parts or not path.parts or path.parts[0] != 'mai'
                    or stat.S_IFMT(item.external_attr >> 16) not in (0, stat.S_IFREG, stat.S_IFDIR)
                    or (item.external_attr >> 16) & 0o7000):
                raise ValueError('Unsafe or duplicate archive entry')
            seen.add(item.filename)
            if not item.is_dir():
                if len(path.parts) < 2 or str(path) != item.filename:
                    raise ValueError('Invalid archive path')
                files[item.filename] = item
        manifest_item = files.get('mai/SHA256SUMS')
        if not manifest_item or manifest_item.file_size > 1024 * 1024:
            raise ValueError('Missing or oversized SHA256SUMS')
        manifest = package.read(manifest_item)
        signature = files.get('mai/SHA256SUMS.sig')
        if signature:
            if not trusted_key or signature.file_size != 64:
                raise ValueError('Signed archive requires an independently trusted public key')
            raw_key = base64.b64decode(trusted_key, validate=True)
            if len(raw_key) != 32:
                raise ValueError('Public key must be base64-encoded raw Ed25519 (32 bytes)')
            with tempfile.TemporaryDirectory(prefix='mai-verify-') as directory:
                directory = pathlib.Path(directory)
                (directory / 'key.der').write_bytes(bytes.fromhex('302a300506032b6570032100') + raw_key)
                (directory / 'checksums').write_bytes(manifest)
                (directory / 'signature').write_bytes(package.read(signature))
                result = subprocess.run(['openssl', 'pkeyutl', '-verify', '-pubin', '-keyform', 'DER',
                                         '-inkey', str(directory / 'key.der'), '-rawin',
                                         '-in', str(directory / 'checksums'), '-sigfile', str(directory / 'signature')],
                                        capture_output=True, timeout=30, check=False)
                if result.returncode:
                    raise ValueError('Signature verification failed')
        elif not allow_unsigned or trusted_key:
            raise ValueError('Unsigned archive rejected; trusted-local verification needs --allow-unsigned')
        declared = {}
        for line in manifest.decode('ascii').splitlines():
            match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9_./-]+)', line)
            if not match:
                raise ValueError('Invalid checksum manifest')
            name = 'mai/' + match[2]
            if name in declared:
                raise ValueError('Duplicate checksum entry')
            declared[name] = match[1]
        payload = set(files) - {'mai/SHA256SUMS', 'mai/SHA256SUMS.sig'}
        if not payload or payload != set(declared):
            raise ValueError('Package contains missing or unlisted payload files')
        for name, expected in declared.items():
            digest = hashlib.sha256()
            with package.open(files[name]) as source:
                for chunk in iter(lambda: source.read(65536), b''):
                    digest.update(chunk)
            if digest.hexdigest() != expected:
                raise ValueError('Payload checksum mismatch: ' + name)
        return sorted(payload)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('archive')
    parser.add_argument('--trusted-key', help='Public key copied through a trusted channel, not from the archive')
    parser.add_argument('--allow-unsigned', action='store_true', help='Explicit trusted-local mode; checks integrity, not authenticity')
    args = parser.parse_args()
    try:
        files = verify(args.archive, args.trusted_key, args.allow_unsigned)
    except (ValueError, OSError, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        parser.exit(1, 'Verification failed: ' + str(error) + '\n')
    print('Verified %d payload files before extraction.' % len(files))


if __name__ == '__main__':
    main()
