import base64
import hashlib
import importlib.util
import pathlib
import subprocess
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('verify_mai', pathlib.Path(__file__).with_name('verify-mai-package.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class VerifyPackageTests(unittest.TestCase):
    def test_integrity_and_unsigned_opt_in(self):
        with tempfile.TemporaryDirectory() as temp:
            path = pathlib.Path(temp) / 'mai.zip'
            content = b'example binary'
            manifest = (hashlib.sha256(content).hexdigest() + '  maiservice\n').encode()
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr('mai/maiservice', content)
                archive.writestr('mai/SHA256SUMS', manifest)
            with self.assertRaises(ValueError): module.verify(path)
            self.assertEqual(module.verify(path, allow_unsigned=True), ['mai/maiservice'])
            with zipfile.ZipFile(path, 'a') as archive: archive.writestr('mai/config/workflows/injected.yaml', 'bad')
            with self.assertRaises(ValueError): module.verify(path, allow_unsigned=True)

    def test_signature_and_tampering(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            subprocess.run(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(root / 'key')], check=True, capture_output=True)
            public = subprocess.check_output(['openssl', 'pkey', '-in', str(root / 'key'), '-pubout', '-outform', 'DER'])[-32:]
            payload = b'example binary'
            manifest = (hashlib.sha256(payload).hexdigest() + '  maiservice\n').encode()
            (root / 'manifest').write_bytes(manifest)
            subprocess.run(['openssl', 'pkeyutl', '-sign', '-inkey', str(root / 'key'), '-rawin', '-in', str(root / 'manifest'), '-out', str(root / 'signature')], check=True, capture_output=True)
            for content, valid in [(payload, True), (b'tampered', False)]:
                with zipfile.ZipFile(root / 'mai.zip', 'w') as archive:
                    archive.writestr('mai/maiservice', content)
                    archive.writestr('mai/SHA256SUMS', manifest)
                    archive.writestr('mai/SHA256SUMS.sig', (root / 'signature').read_bytes())
                if valid: module.verify(root / 'mai.zip', base64.b64encode(public).decode())
                else:
                    with self.assertRaises(ValueError): module.verify(root / 'mai.zip', base64.b64encode(public).decode())

    def test_traversal_and_symlinks(self):
        with tempfile.TemporaryDirectory() as temp:
            for name, mode in [('mai/../escape', 0), ('mai/link', 0o120777)]:
                path = pathlib.Path(temp) / 'mai.zip'
                with zipfile.ZipFile(path, 'w') as archive:
                    info = zipfile.ZipInfo(name)
                    info.external_attr = mode << 16
                    archive.writestr(info, b'bad')
                with self.assertRaises(ValueError): module.verify(path, allow_unsigned=True)


if __name__ == '__main__': unittest.main()
