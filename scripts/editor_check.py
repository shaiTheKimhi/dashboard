#!/usr/bin/env python3
"""Check the real browser editor using disposable files and local DevTools.

Uses the same Chromium setup as browser_check.py. Reads private credentials
without printing them. SFTP links are inspected, not launched on this device.
"""
import base64
import json
import os
from pathlib import Path
import shutil
import tempfile
import urllib.parse
import urllib.request

from browser_check import DevTools, wait_for


def main():
    config = Path.home() / '.config/jetson-dashboard'
    credentials = dict(line.split('=', 1) for line in (config / 'credentials.txt').read_text().splitlines())
    env = dict(line.split('=', 1) for line in (config / 'dashboard.env').read_text().splitlines())
    export = Path(env['DASHBOARD_FILE_ROOT'].strip('"'))
    fixture = Path(tempfile.mkdtemp(prefix='.dashboard-editor-check-', dir=str(export)))
    note = fixture / 'שלום # note.txt'
    note.write_text('before\n')
    note.chmod(0o640)
    (fixture / 'binary.bin').write_bytes(b'\x00\xff\x01')
    (fixture / 'large.txt').write_text('x' * ((1 << 20) + 1))
    checks = []
    try:
        targets = json.loads(urllib.request.urlopen('http://127.0.0.1:19223/json/list').read())
        cdp = DevTools(next(t['webSocketDebuggerUrl'] for t in targets if t['type'] == 'page'))
        for domain in ['Network', 'Page', 'Runtime']:
            cdp.call(domain + '.enable')
        auth = base64.b64encode((credentials['username'] + ':' + credentials['password']).encode()).decode()
        cdp.call('Network.setExtraHTTPHeaders', {'headers': {'Authorization': 'Basic ' + auth}})
        cdp.call('Emulation.setDeviceMetricsOverride', {'width': 1440, 'height': 1200, 'deviceScaleFactor': 1, 'mobile': False})
        cdp.call('Page.navigate', {'url': 'https://' + env['DASHBOARD_LISTEN'].strip('"')})
        wait_for(cdp, "document.querySelector('#refresh-files') && !document.querySelector('#refresh-files').disabled")
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled")
        cdp.evaluate("Array.from(document.querySelectorAll('.directory-button')).find(b => b.textContent === " + json.dumps(fixture.name) + ").click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled")

        def click_action(name, action):
            cdp.evaluate("Array.from(document.querySelectorAll('#file-rows tr')).find(r => Array.from(r.querySelectorAll('a[download]')).some(a => a.getAttribute('download') === " + json.dumps(name) + ")).querySelector('." + action + "-button').click()")

        def change_text(content):
            cdp.evaluate("document.querySelector('#editor-content').value = " + json.dumps(content) + "; document.querySelector('#editor-content').dispatchEvent(new Event('input'))")

        assert cdp.evaluate("document.querySelectorAll('.edit-button').length === 3"), 'Edit actions are missing'
        click_action(note.name, 'edit')
        wait_for(cdp, "document.querySelector('#editor-dialog').open && !document.querySelector('#editor-content').disabled")
        assert cdp.evaluate("document.querySelector('#editor-content').value === 'before\\n'")
        change_text('saved שלום\n')
        cdp.evaluate("document.querySelector('#editor-save').click()")
        wait_for(cdp, "document.querySelector('#editor-status').textContent === 'Saved.'")
        assert note.read_text() == 'saved שלום\n'
        assert note.stat().st_mode & 0o777 == 0o640
        # A second save must use the revision returned by the first save.
        change_text('saved twice\n')
        cdp.evaluate("document.querySelector('#editor-save').click()")
        wait_for(cdp, "document.querySelector('#editor-status').textContent === 'Saved.'")
        assert note.read_text() == 'saved twice\n'
        checks.append('Unicode path opens and saves repeatedly while preserving permissions')
        change_text('unsaved\n')
        cdp.evaluate("window.confirm = () => false; document.querySelector('#editor-cancel').click()")
        assert cdp.evaluate("document.querySelector('#editor-dialog').open && document.querySelector('#editor-content').value === 'unsaved\\n'")
        cdp.evaluate("window.confirm = () => true; document.querySelector('#editor-cancel').click()")
        assert cdp.evaluate("!document.querySelector('#editor-dialog').open")
        assert note.read_text() == 'saved twice\n'
        assert cdp.evaluate("(() => { const event = new Event('beforeunload', {cancelable:true}); window.dispatchEvent(event); return !event.defaultPrevented; })()")
        checks.append('Cancel warns about unsaved changes and discards only when confirmed')
        click_action(note.name, 'edit')
        wait_for(cdp, "!document.querySelector('#editor-content').disabled")
        note.write_text('external change\n')
        change_text('local edits\n')
        cdp.evaluate("document.querySelector('#editor-save').click()")
        wait_for(cdp, "document.querySelector('#editor-status').textContent.includes('changed since')")
        assert note.read_text() == 'external change\n'
        assert cdp.evaluate("document.querySelector('#editor-content').value === 'local edits\\n'")
        cdp.evaluate("document.querySelector('#editor-cancel').click()")
        checks.append('Conflicting external changes are preserved and unsaved text remains available')
        for name, expected in [('binary.bin', 'UTF-8'), ('large.txt', '1 MiB')]:
            click_action(name, 'edit')
            wait_for(cdp, "document.querySelector('#editor-status').textContent.includes(" + json.dumps(expected) + ")")
            assert cdp.evaluate("document.querySelector('#editor-save').disabled")
            cdp.evaluate("document.querySelector('#editor-cancel').click()")
        checks.append('Binary and oversized files show useful errors and cannot be saved')
        click_action(note.name, 'external')
        wait_for(cdp, "document.querySelector('#external-dialog').open && document.querySelector('#external-url').value.startsWith('sftp://')")
        link = urllib.parse.urlsplit(cdp.evaluate("document.querySelector('#external-url').value"))
        assert urllib.parse.unquote(link.path) == str(note)
        assert urllib.parse.unquote(link.username) == 'shai'
        assert link.port == 22
        assert cdp.evaluate("document.querySelector('#external-path').value") == str(note)
        assert cdp.evaluate("document.querySelector('#external-open').href === document.querySelector('#external-url').value")
        checks.append('External edit provides encoded SFTP link and exact remote path')
        cdp.call('Emulation.setDeviceMetricsOverride', {'width': 390, 'height': 844, 'deviceScaleFactor': 1, 'mobile': True})
        assert cdp.evaluate("document.querySelector('#external-dialog').getBoundingClientRect().right <= innerWidth && document.documentElement.scrollWidth <= innerWidth")
        cdp.evaluate("document.querySelector('#external-close').click()")
        click_action(note.name, 'edit')
        wait_for(cdp, "!document.querySelector('#editor-content').disabled")
        assert cdp.evaluate("document.querySelector('#editor-dialog').getBoundingClientRect().right <= innerWidth && document.querySelector('#editor-content').getBoundingClientRect().right <= innerWidth")
        cdp.evaluate("document.querySelector('#editor-cancel').click()")
        assert not cdp.exceptions, repr(cdp.exceptions)
        checks.append('Editor and external-edit dialogs fit a 390px viewport')
        Path('build/editor-check.json').write_text(json.dumps({'checks': checks}, indent=2) + '\n')
        print(str(len(checks)) + ' editor browser checks passed')
        for check in checks:
            print('  ' + check)
        cdp.call('Page.navigate', {'url': 'about:blank'})
    finally:
        shutil.rmtree(str(fixture))


if __name__ == '__main__':
    main()
