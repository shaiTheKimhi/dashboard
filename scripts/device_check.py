#!/usr/bin/env python3
"""Check the installed user service, streaming downloads and resource usage."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import ssl
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--seconds', type=int, default=300)
    args = parser.parse_args()
    config = Path.home() / '.config/jetson-dashboard'
    env = dict(line.split('=', 1) for line in (config / 'dashboard.env').read_text().splitlines())
    credentials = dict(line.split('=', 1) for line in (config / 'credentials.txt').read_text().splitlines())
    url = 'https://' + env['DASHBOARD_LISTEN'].strip('"')
    context = ssl.create_default_context(cafile=str(config / 'cert.pem'))
    auth = 'Basic ' + base64.b64encode((credentials['username'] + ':' + credentials['password']).encode()).decode()
    def get(path, authenticated=True):
        req = urllib.request.Request(url + path, headers={'Authorization': auth} if authenticated else {})
        return urllib.request.urlopen(req, context=context, timeout=10)
    for route in ['/', '/healthz', '/static/app.js', '/api/system', '/api/files']:
        try:
            get(route, False)
            raise AssertionError('Missing authentication boundary')
        except urllib.error.HTTPError as error:
            assert error.code == 401
        with get(route) as response:
            assert response.status == 200
    print('All page/API/asset routes require authentication; trusted HTTPS works', flush=True)
    pid = int(subprocess.check_output(['systemctl', '--user', 'show', 'jetson-dashboard.service', '-p', 'MainPID', '--value']))
    def usage():
        stat = Path('/proc/{}/stat'.format(pid)).read_text().split(') ', 1)[1].split()
        ticks = int(stat[11]) + int(stat[12])
        status = Path('/proc/{}/status'.format(pid)).read_text().splitlines()
        rss = int(next(line.split()[1] for line in status if line.startswith('VmRSS:'))) * 1024
        return ticks, rss
    baseline = usage()[1]
    export = Path(env['DASHBOARD_FILE_ROOT'].strip('"'))
    transfer = export / '.dashboard-transfer-check.bin'
    block = bytes(range(256)) * 4096
    source_hash = hashlib.sha256()
    try:
        with transfer.open('wb') as output:
            for _ in range(128):
                output.write(block)
                source_hash.update(block)
        received = hashlib.sha256()
        peak = baseline
        with get('/api/download?' + urllib.parse.urlencode({'path': transfer.name})) as response:
            while True:
                chunk = response.read(65536)
                if not chunk:
                    break
                received.update(chunk)
                peak = max(peak, usage()[1])
        assert source_hash.digest() == received.digest(), 'Download hash mismatch'
        assert peak - baseline < 32 * 1024 * 1024, 'Download memory scaled unexpectedly'
        with get('/api/download?' + urllib.parse.urlencode({'path': transfer.name})) as response:
            response.read(1024)  # Closing early exercises client cancellation.
        with get('/api/system') as response:
            assert json.load(response)['identity']['model'].startswith('NVIDIA Jetson Nano')
        print('128 MiB download SHA-256 matched; interrupted transfer leaves metrics responsive', flush=True)
    finally:
        transfer.unlink()
    samples = []
    start = time.monotonic()
    start_ticks = usage()[0]
    while time.monotonic() - start < args.seconds:
        with get('/api/system') as response:
            data = json.load(response)
            assert not data['stale']
        samples.append(usage()[1])
        time.sleep(2)
    import os
    duration = time.monotonic() - start
    cpu = 100 * (usage()[0] - start_ticks) / os.sysconf('SC_CLK_TCK') / duration
    report = {'duration_seconds': round(duration, 2), 'poll_interval_seconds': 2, 'samples': len(samples),
              'max_rss_mib': round(max(samples) / 1048576, 2), 'cpu_percent_of_one_core': round(cpu, 3),
              'download_size_mib': 128, 'download_peak_rss_mib': round(peak / 1048576, 2),
              'download_rss_growth_mib': round((peak - baseline) / 1048576, 2),
              'sha256_matched': True, 'notes': 'One synthetic visible-client polling load; no physical second-device/reboot test.'}
    Path('build/device-check.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2), flush=True)
    assert report['max_rss_mib'] < 100 and cpu < 5, 'Resource target exceeded'


if __name__ == '__main__':
    main()
