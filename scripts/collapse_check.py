#!/usr/bin/env python3
"""Check panel disclosure controls in the deployed dashboard (read-only)."""
import base64
import json
from pathlib import Path
import urllib.request

from browser_check import DevTools, wait_for


def main():
    config = Path.home() / '.config/jetson-dashboard'
    credentials = dict(line.split('=', 1) for line in (config / 'credentials.txt').read_text().splitlines())
    env = dict(line.split('=', 1) for line in (config / 'dashboard.env').read_text().splitlines())
    targets = json.loads(urllib.request.urlopen('http://127.0.0.1:19223/json/list').read())
    cdp = DevTools(next(t['webSocketDebuggerUrl'] for t in targets if t['type'] == 'page'))
    for domain in ['Network', 'Page', 'Runtime']:
        cdp.call(domain + '.enable')
    auth = base64.b64encode((credentials['username'] + ':' + credentials['password']).encode()).decode()
    cdp.call('Network.setExtraHTTPHeaders', {'headers': {'Authorization': 'Basic ' + auth}})
    cdp.call('Emulation.setDeviceMetricsOverride', {'width': 1440, 'height': 1200, 'deviceScaleFactor': 1, 'mobile': False})
    cdp.call('Page.navigate', {'url': 'https://' + env['DASHBOARD_LISTEN'].strip('"')})
    wait_for(cdp, "document.querySelector('#connection-status')?.textContent === 'Live connection' && !document.querySelector('#refresh-files')?.disabled")
    checks = []
    assert cdp.evaluate("document.querySelectorAll('details.metric-card, details.panel').length === 8"), 'Not all eight panels are collapsible'
    assert cdp.evaluate("Array.from(document.querySelectorAll('details')).every(p => p.open)"), 'Panels should start expanded'
    # Each main panel must shrink independently and retain its labelled heading.
    assert cdp.evaluate("Array.from(document.querySelectorAll('details')).every(p => { const h = p.getBoundingClientRect().height; p.querySelector('summary').click(); const shrank = !p.open && p.getBoundingClientRect().height < h && p.querySelector('summary h2').getBoundingClientRect().height > 0; p.querySelector('summary').click(); return shrank && p.open; })"), 'A panel did not shrink or restore'
    checks.append('All eight panels collapse independently, retain headings and expand again')
    cdp.evaluate("document.querySelector('.system-panel summary').focus()")
    cdp.call('Input.dispatchKeyEvent', {'type': 'keyDown', 'key': 'Enter', 'code': 'Enter', 'windowsVirtualKeyCode': 13, 'text': '\r', 'unmodifiedText': '\r'})
    cdp.call('Input.dispatchKeyEvent', {'type': 'keyUp', 'key': 'Enter', 'code': 'Enter', 'windowsVirtualKeyCode': 13})
    assert cdp.evaluate("!document.querySelector('.system-panel').open && document.activeElement === document.querySelector('.system-panel summary')")
    cdp.call('Input.dispatchKeyEvent', {'type': 'keyDown', 'key': ' ', 'code': 'Space', 'windowsVirtualKeyCode': 32, 'text': ' ', 'unmodifiedText': ' '})
    cdp.call('Input.dispatchKeyEvent', {'type': 'keyUp', 'key': ' ', 'code': 'Space', 'windowsVirtualKeyCode': 32})
    assert cdp.evaluate("document.querySelector('.system-panel').open")
    checks.append('Enter and Space operate a focused panel heading')
    cdp.evaluate("document.querySelector('.files-panel summary').click()")
    # Chromium can retain layout rectangles for closed-details descendants;
    # verify real hit testing and keyboard navigation instead.
    cdp.evaluate("document.querySelector('.files-panel summary').scrollIntoView(); document.querySelector('.files-panel summary').focus()")
    assert cdp.evaluate("!document.querySelector('.files-panel').open && (() => { const b = document.querySelector('#new-file'), r = b.getBoundingClientRect(); return document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2) !== b; })()")
    cdp.call('Input.dispatchKeyEvent', {'type': 'keyDown', 'key': 'Tab', 'code': 'Tab', 'windowsVirtualKeyCode': 9})
    cdp.call('Input.dispatchKeyEvent', {'type': 'keyUp', 'key': 'Tab', 'code': 'Tab', 'windowsVirtualKeyCode': 9})
    assert cdp.evaluate("!document.activeElement.closest('.files-panel')"), 'Keyboard entered collapsed file controls'
    cdp.evaluate("document.querySelector('.files-panel summary').click(); document.querySelector('#new-file').click()")
    assert cdp.evaluate("document.querySelector('#create-dialog').open")
    cdp.evaluate("document.querySelector('#create-cancel').click()")
    checks.append('Collapsed file controls are hidden and creation still opens after expanding')
    cdp.evaluate("window.sampleBeforeCollapse = document.querySelector('#last-update').textContent; window.metricCount = 0; window.collapseFetch = window.fetch; window.fetch = (...args) => { if (String(args[0]) === '/api/system') window.metricCount++; return collapseFetch(...args); }; document.querySelectorAll('details').forEach(p => p.open = false)")
    wait_for(cdp, "window.metricCount >= 1 && document.querySelector('#connection-status').textContent === 'Live connection'")
    assert cdp.evaluate("Array.from(document.querySelectorAll('details')).every(p => !p.open)"), 'Telemetry reopened panels'
    cdp.evaluate("window.fetch = window.collapseFetch; document.querySelectorAll('details').forEach(p => p.open = true)")
    checks.append('Live polling continues without reopening collapsed panels')
    cdp.call('Emulation.setDeviceMetricsOverride', {'width': 390, 'height': 844, 'deviceScaleFactor': 1, 'mobile': True})
    assert cdp.evaluate("document.documentElement.scrollWidth <= innerWidth")
    cdp.evaluate("document.querySelectorAll('details').forEach(p => p.open = false)")
    assert cdp.evaluate("document.documentElement.scrollWidth <= innerWidth && Array.from(document.querySelectorAll('summary')).every(s => s.getBoundingClientRect().right <= innerWidth)")
    Path('build/dashboard-collapsed-mobile.png').write_bytes(base64.b64decode(cdp.call('Page.captureScreenshot', {'format': 'png'})['data']))
    cdp.evaluate("document.querySelectorAll('details').forEach(p => p.open = true)")
    assert not cdp.exceptions, repr(cdp.exceptions)
    checks.append('Expanded and collapsed layouts fit a 390px viewport')
    Path('build/collapse-check.json').write_text(json.dumps({'checks': checks}, indent=2) + '\n')
    print(str(len(checks)) + ' collapse browser checks passed')
    for check in checks:
        print('  ' + check)
    cdp.call('Page.navigate', {'url': 'about:blank'})


if __name__ == '__main__':
    main()
