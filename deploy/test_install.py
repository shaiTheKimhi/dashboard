import importlib.util
import os
from pathlib import Path
import stat
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('dashboard_install', str(Path(__file__).with_name('install.py')))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallTests(unittest.TestCase):
    def test_system_binary_access_with_private_umask(self):
        """Catch a root-only install directory blocking the service account."""
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            source = base / 'source'
            source.write_bytes(b'release binary')
            destination = base / 'install'
            previous = os.umask(0o077)
            try:
                installer.install_binary(source, destination, system=True)
            finally:
                os.umask(previous)
            self.assertTrue(destination.stat().st_mode & stat.S_IXOTH)
            binary = destination / 'jetson-dashboard'
            self.assertTrue(binary.stat().st_mode & stat.S_IROTH)
            self.assertTrue(binary.stat().st_mode & stat.S_IXOTH)
            self.assertEqual(binary.read_bytes(), b'release binary')
            self.assertFalse((destination / 'jetson-dashboard.new').exists())


if __name__ == '__main__':
    unittest.main()
