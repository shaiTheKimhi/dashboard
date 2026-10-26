#!/usr/bin/env python3
"""Browser smoke checks through a local Chromium DevTools port (stdlib only).

Start Chromium with --headless --disable-gpu --ignore-certificate-errors
--remote-debugging-port=19223 --user-data-dir=<temporary-directory> about:blank.
Only use a disposable browser profile. Reads the local private credentials;
never prints them. Screenshots and the JSON report go under build/.
"""
import base64
import json
import os
from pathlib import Path
import socket
import struct
import time
import urllib.request
import urllib.parse


class DevTools:
    def __init__(self, url):
        u = urllib.parse.urlsplit(url)
        self.socket = socket.create_connection((u.hostname, u.port), timeout=15)
        key = base64.b64encode(os.urandom(16)).decode()
        self.socket.sendall(('GET {} HTTP/1.1\r\nHost: {}:{}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {}\r\nSec-WebSocket-Version: 13\r\n\r\n'.format(u.path, u.hostname, u.port, key)).encode())
        headers = b''
        while not headers.endswith(b'\r\n\r\n'):
            headers += self.socket.recv(1)
        assert headers.split(b'\r\n', 1)[0].split()[1] == b'101', 'DevTools handshake failed'
        self.reader = self.socket.makefile('rb')
        self.counter = 0
        self.exceptions = []

    def send(self, data, opcode=1):
        data = data.encode() if isinstance(data, str) else data
        mask = os.urandom(4)
        header = bytes([0x80 | opcode])
        if len(data) < 126:
            header += bytes([0x80 | len(data)])
        elif len(data) < 65536:
            header += b'\xfe' + struct.pack('!H', len(data))
        else:
            header += b'\xff' + struct.pack('!Q', len(data))
        self.socket.sendall(header + mask + bytes(v ^ mask[i % 4] for i, v in enumerate(data)))

    def receive(self):
        first = self.reader.read(2)
        if len(first) != 2:
            raise RuntimeError('DevTools connection closed')
        size = first[1] & 127
        if size == 126:
            size = struct.unpack('!H', self.reader.read(2))[0]
        elif size == 127:
            size = struct.unpack('!Q', self.reader.read(8))[0]
        data = self.reader.read(size)
        if first[0] & 15 == 9:
            self.send(data, 10)
            return self.receive()
        if first[0] & 15 != 1:
            raise RuntimeError('Unexpected DevTools frame')
        return json.loads(data.decode())

    def call(self, method, params=None):
        self.counter += 1
        self.send(json.dumps({'id': self.counter, 'method': method, 'params': params or {}}))
        while True:
            msg = self.receive()
            if msg.get('method') == 'Runtime.exceptionThrown':
                self.exceptions.append(msg['params']['exceptionDetails'].get('text', 'browser exception'))
            if msg.get('id') == self.counter:
                if 'error' in msg:
                    raise RuntimeError(msg['error'])
                return msg.get('result', {})

    def evaluate(self, expression):
        result = self.call('Runtime.evaluate', {'expression': expression, 'returnByValue': True, 'awaitPromise': True})
        if 'exceptionDetails' in result:
            raise AssertionError(result['exceptionDetails'].get('exception', {}).get('description', 'Browser assertion failed'))
        return result.get('result', {}).get('value')


def wait_for(cdp, expression):
    for _ in range(100):
        if cdp.evaluate(expression):
            return
        time.sleep(.1)
    raise AssertionError('Timed out waiting for: ' + expression)


def main():
    config = Path.home() / '.config/jetson-dashboard'
    credentials = dict(line.split('=', 1) for line in (config / 'credentials.txt').read_text().splitlines())
    env = dict(line.split('=', 1) for line in (config / 'dashboard.env').read_text().splitlines())
    url = 'https://' + env['DASHBOARD_LISTEN'].strip('"')
    export = Path(env['DASHBOARD_FILE_ROOT'].strip('"'))
    fixture = export / '.dashboard-browser-check'
    fixture.mkdir(exist_ok=True)
    special_name = '<img src=x onerror=alert(1)> שלום.txt'
    (fixture / special_name).write_text('Browser download check\n')
    (fixture / 'empty').mkdir(exist_ok=True)
    (fixture / 'other').mkdir(exist_ok=True)
    (fixture / '.hidden.txt').write_text('Hidden file\n')
    (fixture / '.hidden-folder').mkdir(exist_ok=True)
    (fixture / '.hidden-folder' / '.inside.txt').write_text('Hidden child\n')
    targets = json.loads(urllib.request.urlopen('http://127.0.0.1:19223/json/list').read())
    cdp = DevTools(next(t['webSocketDebuggerUrl'] for t in targets if t['type'] == 'page'))
    auth = base64.b64encode((credentials['username'] + ':' + credentials['password']).encode()).decode()
    cdp.call('Network.enable')
    cdp.call('Page.enable')
    cdp.call('Runtime.enable')
    cdp.call('Network.setExtraHTTPHeaders', {'headers': {'Authorization': 'Basic ' + auth}})
    cdp.call('Emulation.setDeviceMetricsOverride', {'width': 1440, 'height': 1200, 'deviceScaleFactor': 1, 'mobile': False})
    cdp.call('Page.navigate', {'url': url})
    checks = []
    try:
        for _ in range(100):
            if cdp.evaluate("document.querySelector('#connection-status')?.textContent === 'Live connection' && !document.querySelector('#refresh-files')?.disabled"):
                break
            time.sleep(.1)
        else:
            raise AssertionError('Dashboard did not connect')
        checks.append('Authenticated page loads live telemetry and files')
        assert cdp.evaluate("document.querySelector('#show-hidden') !== null"), 'Show hidden files checkbox is missing'
        assert cdp.evaluate("!document.querySelector('#show-hidden').checked && !document.querySelector('#file-rows').textContent.includes('.dashboard-browser-check')"), 'Hidden folders are visible by default'
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && Array.from(document.querySelectorAll('.directory-button')).some(b => b.textContent === '.dashboard-browser-check')")
        # Open the real folder through its rendered button and check injection.
        cdp.evaluate("Array.from(document.querySelectorAll('.directory-button')).find(b => b.textContent === '.dashboard-browser-check').click()")
        for _ in range(100):
            if cdp.evaluate("document.querySelector('#file-rows').textContent.includes('שלום.txt')"):
                break
            time.sleep(.1)
        else:
            raise AssertionError('Special filename did not render')
        assert cdp.evaluate("document.querySelector('#file-count').textContent === '5 items' && document.querySelector('#file-rows').textContent.includes('.hidden.txt') && document.querySelector('#file-rows').textContent.includes('.hidden-folder')")
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && document.querySelector('#file-count').textContent === '3 items'")
        assert cdp.evaluate("!document.querySelector('#file-rows').textContent.includes('.hidden.txt') && !document.querySelector('#file-rows').textContent.includes('.hidden-folder') && document.querySelector('#file-rows').textContent.includes('.. Parent directory')")
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && document.querySelector('#file-count').textContent === '5 items'")
        cdp.evaluate("Array.from(document.querySelectorAll('.directory-button')).find(b => b.textContent === '.hidden-folder').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && document.querySelector('#file-rows').textContent.includes('.inside.txt')")
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && document.querySelector('#file-count').textContent === '0 items'")
        assert cdp.evaluate("!document.querySelector('#file-status').hidden && document.querySelector('#file-status').textContent.toLowerCase().includes('hidden') && document.querySelector('#file-rows').textContent.includes('.. Parent directory')")
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && document.querySelector('#file-rows').textContent.includes('.inside.txt')")
        cdp.evaluate("document.querySelector('.directory-button').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled && document.querySelector('#file-rows').textContent.includes('שלום.txt')")
        checks.append('Hidden files and folders toggle together, with correct counts, navigation and hidden-only state')
        assert cdp.evaluate("document.querySelectorAll('#file-rows img').length === 0"), 'Filename injected markup'
        assert cdp.evaluate("document.querySelector('#file-rows').textContent.includes('<img src=x onerror=alert(1)>')")
        assert cdp.evaluate("fetch(Array.from(document.querySelectorAll('a.download-link')).find(a => a.getAttribute('download').includes('שלום')).href).then(r=>r.text()).then(t=>t === 'Browser download check\\n')")
        checks.append('HTML-like/Unicode filenames render literally and download correctly')
        cdp.evaluate("Array.from(document.querySelectorAll('.directory-button')).find(b => b.textContent === 'empty').click()")
        time.sleep(.4)
        assert cdp.evaluate("document.querySelector('#file-status').textContent === 'This folder is empty.' && !document.querySelector('#file-status').hidden")
        checks.append('Empty folder and parent navigation work')
        cdp.evaluate("document.querySelector('.directory-button').click()")
        time.sleep(.4)
        # Delay one response, then navigate again. The obsolete folder must not win.
        cdp.evaluate("window.originalFetch = window.fetch; window.fetch = async (...args) => { const r = await originalFetch(...args); if (String(args[0]).includes('path=.dashboard-browser-check%2Fempty')) await new Promise(resolve=>setTimeout(resolve,700)); return r; }; Array.from(document.querySelectorAll('.directory-button')).find(b=>b.textContent==='empty').click(); document.querySelector('#breadcrumbs button').click();")
        time.sleep(1)
        assert cdp.evaluate("document.querySelector('#breadcrumbs').textContent.trim() === 'Shared files'"), 'Old navigation response won'
        cdp.evaluate('window.fetch = window.originalFetch')
        checks.append('Rapid navigation ignores obsolete directory responses')
        png = cdp.call('Page.captureScreenshot', {'format': 'png', 'captureBeyondViewport': False})['data']
        Path('build/dashboard-desktop.png').write_bytes(base64.b64decode(png))
        cdp.call('Emulation.setDeviceMetricsOverride', {'width': 390, 'height': 844, 'deviceScaleFactor': 1, 'mobile': True})
        time.sleep(.3)
        assert cdp.evaluate('document.documentElement.scrollWidth <= window.innerWidth'), 'Page overflows mobile viewport'
        png = cdp.call('Page.captureScreenshot', {'format': 'png'})['data']
        Path('build/dashboard-mobile.png').write_bytes(base64.b64decode(png))
        checks.append('390px mobile viewport has no page overflow')
        cdp.evaluate("window.metricRequests = 0; window.fetch = (...args) => { if (String(args[0]) === '/api/system') window.metricRequests++; return originalFetch(...args); }; Object.defineProperty(document,'hidden',{configurable:true,get:()=>true}); document.dispatchEvent(new Event('visibilitychange'));")
        time.sleep(2.3)
        assert cdp.evaluate('window.metricRequests === 0'), 'Hidden page kept polling'
        cdp.evaluate("Object.defineProperty(document,'hidden',{configurable:true,get:()=>false}); document.dispatchEvent(new Event('visibilitychange'));")
        time.sleep(.5)
        assert cdp.evaluate('window.metricRequests >= 1'), 'Visible page did not refresh'
        checks.append('Visibility event pauses polling and refreshes on return')
        cdp.call('Network.emulateNetworkConditions', {'offline': True, 'latency': 0, 'downloadThroughput': 0, 'uploadThroughput': 0})
        time.sleep(7)
        assert cdp.evaluate("document.querySelector('#connection-status').classList.contains('stale') && document.querySelector('#cpu-value').textContent !== '—'")
        cdp.call('Network.emulateNetworkConditions', {'offline': False, 'latency': 0, 'downloadThroughput': -1, 'uploadThroughput': -1})
        for _ in range(60):
            if cdp.evaluate("document.querySelector('#connection-status').textContent === 'Live connection'"):
                break
            time.sleep(.1)
        else:
            raise AssertionError('No reconnection')
        checks.append('Offline state preserves values, marks stale and reconnects')
        assert not cdp.exceptions, 'Browser runtime errors: ' + repr(cdp.exceptions)
        version = cdp.call('Browser.getVersion')['product']
        Path('build/browser-check.json').write_text(json.dumps({'browser': version, 'checks': checks}, indent=2) + '\n')
        print(version + ': ' + str(len(checks)) + ' browser checks passed')
        for check in checks:
            print('  ' + check)
        cdp.call('Page.navigate', {'url': 'about:blank'})
    finally:
        (fixture / '.hidden-folder' / '.inside.txt').unlink()
        for item in fixture.iterdir():
            if item.is_dir():
                item.rmdir()
            else:
                item.unlink()
        fixture.rmdir()


if __name__ == '__main__':
    main()
