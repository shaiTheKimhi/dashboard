#!/usr/bin/env python3
"""Install the built binary and a user or dedicated-account systemd service."""
import argparse
import hashlib
import ipaddress
import os
from pathlib import Path
import pwd
import secrets
import shutil
import socket
import subprocess


def run(*args):
    subprocess.check_call([str(a) for a in args])


def install_binary(source, install, system=False):
    install.mkdir(parents=True, exist_ok=True)
    if system:
        # The private umask must not prevent the service account traversing /opt.
        install.chmod(0o755)
    staging = install / 'jetson-dashboard.new'
    shutil.copyfile(str(source), str(staging))
    staging.chmod(0o755)
    os.replace(str(staging), str(install / 'jetson-dashboard'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--user', action='store_true')
    mode.add_argument('--system', action='store_true')
    parser.add_argument('--ip', required=True, help='Explicit private LAN IPv4 address')
    parser.add_argument('--file-root', default='/home/shai/dashboard-share')
    parser.add_argument('--renew-cert', action='store_true')
    args = parser.parse_args()
    ip = ipaddress.ip_address(args.ip)
    if ip.version != 4 or not ip.is_private or ip.is_unspecified or ip.is_loopback or ip.is_multicast:
        parser.error('--ip must be a private LAN IPv4 address')
    if args.system and os.geteuid() != 0:
        parser.error('--system requires administrator privileges; use sudo or --user')
    if args.user and os.geteuid() == 0:
        parser.error('run --user as the device owner, without sudo')
    project = Path(__file__).resolve().parent.parent
    source = project / 'build/jetson-dashboard'
    if not source.is_file():
        parser.error('build/jetson-dashboard is missing; build the application first')
    export = Path(args.file_root).resolve()
    if not export.is_absolute() or str(export) == '/' or export == Path.home():
        parser.error('choose a dedicated export directory')
    if any(c in str(export) for c in '\n\r\x00'):
        parser.error('invalid export directory')
    os.umask(0o077)
    if args.user:
        config = Path.home() / '.config/jetson-dashboard'
        install = Path.home() / '.local/share/jetson-dashboard'
        unit_dir = Path.home() / '.config/systemd/user'
    else:
        config = Path('/etc/jetson-dashboard')
        install = Path('/opt/jetson-dashboard')
        unit_dir = Path('/etc/systemd/system')
        try:
            pwd.getpwnam('jetson-dashboard')
        except KeyError:
            run('useradd', '--system', '--user-group', '--no-create-home', '--shell', '/usr/sbin/nologin', 'jetson-dashboard')
    for directory in [config, install, unit_dir]:
        directory.mkdir(parents=True, exist_ok=True)
    export.mkdir(parents=True, exist_ok=True, mode=0o750)
    if args.system:
        owner = pwd.getpwnam(os.environ.get('SUDO_USER', 'root'))
        account = pwd.getpwnam('jetson-dashboard')
        # Only the export directory is changed; existing content is left intact.
        os.chown(str(export), owner.pw_uid, account.pw_gid)
        os.chmod(str(export), 0o2750)
        os.chown(str(config), 0, account.pw_gid)
        os.chmod(str(config), 0o750)
    credentials = config / 'credentials.txt'
    if credentials.exists():
        lines = credentials.read_text().splitlines()
        password = next(line.split('=', 1)[1] for line in lines if line.startswith('password='))
        username = next((line.split('=', 1)[1] for line in lines if line.startswith('username=')), 'dashboard')
    else:
        username = 'dashboard'
        password = secrets.token_urlsafe(32)
        credentials.write_text('username=dashboard\npassword=' + password + '\n')
        credentials.chmod(0o600)
    cert, key = config / 'cert.pem', config / 'key.pem'
    if not cert.exists() or not key.exists() or args.renew_cert:
        hostname = socket.gethostname()
        if not all(c.isalnum() or c in '.-' for c in hostname):
            parser.error('hostname contains unsupported certificate characters')
        run('openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256',
            '-nodes', '-days', '365', '-subj', '/CN=' + hostname,
            '-addext', 'subjectAltName=DNS:{},DNS:localhost,IP:{},IP:127.0.0.1'.format(hostname, ip),
            '-keyout', key, '-out', cert)
        key.chmod(0o600)
        cert.chmod(0o644)
    else:
        result = subprocess.call(['openssl', 'x509', '-in', str(cert), '-noout', '-checkip', str(ip)], stdout=subprocess.DEVNULL)
        if result:
            parser.error('certificate does not cover this IP; rerun with --renew-cert and trust the new certificate')
    values = {
        'DASHBOARD_LISTEN': '{}:8443'.format(ip),
        'DASHBOARD_FILE_ROOT': str(export),
        'DASHBOARD_SECRET_SHA256': hashlib.sha256(password.encode()).hexdigest(),
        'DASHBOARD_USERNAME': username,
        'DASHBOARD_TLS_CERT': str(cert), 'DASHBOARD_TLS_KEY': str(key),
    }
    def quote(value):
        return '"' + value.replace('\\', '\\\\').replace('"', '\\"') + '"'
    env = config / 'dashboard.env'
    env.write_text(''.join('{}={}\n'.format(k, quote(v)) for k, v in values.items()))
    env.chmod(0o600)
    binary = install / 'jetson-dashboard'
    install_binary(source, install, system=args.system)
    if args.user:
        # Filesystem namespace hardening needs privileges on this older systemd.
        unit = '''[Unit]
Description=Jetson browser dashboard (user)
After=network.target

[Service]
Type=simple
EnvironmentFile={env}
ExecStart={binary}
Restart=on-failure
RestartSec=5
TimeoutStopSec=15
NoNewPrivileges=yes
UMask=0077

[Install]
WantedBy=default.target
'''.format(env=env, binary=binary)
    else:
        unit = (project / 'deploy/jetson-dashboard.service').read_text()
        os.chown(str(key), 0, account.pw_gid)
        key.chmod(0o640)
    unit_path = unit_dir / 'jetson-dashboard.service'
    unit_path.write_text(unit)
    unit_path.chmod(0o644)
    control = ['systemctl'] + (['--user'] if args.user else [])
    run(*(control + ['daemon-reload']))
    run(*(control + ['enable', 'jetson-dashboard.service']))
    run(*(control + ['restart', 'jetson-dashboard.service']))
    if args.user:
        try:
            run('loginctl', '--no-ask-password', 'enable-linger', pwd.getpwuid(os.getuid()).pw_name)
        except subprocess.CalledProcessError:
            print('Enable linger as an administrator to start before login.')
    print('Dashboard: https://{}:8443'.format(ip))
    print('Credentials (private file): {}'.format(credentials))
    print('Certificate to trust on clients: {}'.format(cert))
    print('Shared directory: {}'.format(export))


if __name__ == '__main__':
    main()
