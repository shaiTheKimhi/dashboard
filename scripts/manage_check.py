#!/usr/bin/env python3
"""Real create/delete browser checks, confined to disposable directories."""
import base64
import json
import os
from pathlib import Path
import shutil
import tempfile
import urllib.request

from browser_check import DevTools, wait_for


def main():
    config = Path.home() / '.config/jetson-dashboard'
    credentials = dict(line.split('=', 1) for line in (config / 'credentials.txt').read_text().splitlines())
    env = dict(line.split('=', 1) for line in (config / 'dashboard.env').read_text().splitlines())
    export = Path(env['DASHBOARD_FILE_ROOT'].strip('"'))
    fixture = Path(tempfile.mkdtemp(prefix='.dashboard-manage-check-', dir=str(export)))
    outside = Path(tempfile.mkdtemp(prefix='dashboard-manage-outside-'))
    sentinel = outside / 'sentinel.txt'
    sentinel.write_text('keep outside\n')
    (fixture / 'keep.txt').write_text('keep\n')
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
        assert cdp.evaluate("document.querySelector('#new-file') !== null && document.querySelector('#new-folder') !== null"), 'Create controls are missing'
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled")
        cdp.evaluate("Array.from(document.querySelectorAll('.directory-button')).find(b => b.textContent === " + json.dumps(fixture.name) + ").click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled")

        def create(name, kind):
            cdp.evaluate("document.querySelector('#new-" + kind + "').click(); document.querySelector('#create-name').value = " + json.dumps(name) + "; document.querySelector('#create-submit').click()")

        def delete(name):
            cdp.evaluate("Array.from(document.querySelectorAll('.delete-button')).find(b => b.getAttribute('aria-label') === " + json.dumps('Delete ' + name) + ").click()")
            wait_for(cdp, "document.querySelector('#delete-dialog').open && document.querySelector('#delete-status').textContent === ''")

        create('שלום folder', 'folder')
        wait_for(cdp, "!document.querySelector('#create-dialog').open && !document.querySelector('#refresh-files').disabled")
        assert (fixture / 'שלום folder').is_dir()
        # Submit directly to exercise the fallback when browser pattern validation
        # is unavailable; a single-name dialog must not accept a nested path.
        cdp.evaluate("document.querySelector('#new-file').click(); document.querySelector('#create-name').value = 'שלום folder/unintended.txt'; document.querySelector('#create-form').dispatchEvent(new Event('submit', {bubbles:true, cancelable:true}))")
        wait_for(cdp, "document.querySelector('#editor-dialog').open || document.querySelector('#create-status').textContent.includes('single name')")
        assert not (fixture / 'שלום folder' / 'unintended.txt').exists(), 'A nested path was accepted as an entry name'
        assert cdp.evaluate("!document.querySelector('#create-name').checkValidity()"), 'Slash-containing name passes native validation'
        cdp.evaluate("document.querySelector('#create-cancel').click()")
        checks.append('Single-name creation rejects slash paths with and without native validation')
        create('new #.txt', 'file')
        wait_for(cdp, "document.querySelector('#editor-dialog').open && !document.querySelector('#editor-content').disabled")
        assert cdp.evaluate("document.querySelector('#editor-content').value === ''")
        cdp.evaluate("document.querySelector('#editor-content').value = 'new contents\\n'; document.querySelector('#editor-content').dispatchEvent(new Event('input')); document.querySelector('#editor-save').click()")
        wait_for(cdp, "document.querySelector('#editor-status').textContent === 'Saved.'")
        cdp.evaluate("document.querySelector('#editor-cancel').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled")
        assert (fixture / 'new #.txt').read_text() == 'new contents\n'
        checks.append('Create a Unicode folder and a file that opens in the editor and saves')
        create('keep.txt', 'file')
        wait_for(cdp, "document.querySelector('#create-status').textContent.includes('already exists')")
        assert (fixture / 'keep.txt').read_text() == 'keep\n'
        cdp.evaluate("document.querySelector('#create-cancel').click(); document.querySelector('#new-file').click(); document.querySelector('#create-name').value = 'cancelled.txt'; document.querySelector('#create-cancel').click()")
        assert not (fixture / 'cancelled.txt').exists()
        checks.append('Duplicate creation preserves the original and cancel creates nothing')
        delete('keep.txt')
        cdp.evaluate("document.querySelector('#delete-cancel').click()")
        assert (fixture / 'keep.txt').exists()
        delete('new #.txt')
        cdp.evaluate("document.querySelector('#delete-confirm').click()")
        wait_for(cdp, "!document.querySelector('#delete-dialog').open && !document.querySelector('#refresh-files').disabled")
        assert not (fixture / 'new #.txt').exists()
        checks.append('File deletion requires confirmation and cancel preserves the file')
        nested = fixture / 'שלום folder' / 'nested'
        nested.mkdir()
        (nested / '.hidden').write_text('hidden\n')
        (nested / 'outside-link').symlink_to(outside, target_is_directory=True)
        delete('שלום folder')
        assert cdp.evaluate("document.querySelector('#delete-confirm').disabled && !document.querySelector('#delete-name-label').hidden")
        cdp.evaluate("document.querySelector('#delete-name').value = 'wrong'; document.querySelector('#delete-name').dispatchEvent(new Event('input'))")
        assert cdp.evaluate("document.querySelector('#delete-confirm').disabled")
        cdp.evaluate("document.querySelector('#delete-name').value = 'שלום folder'; document.querySelector('#delete-name').dispatchEvent(new Event('input')); document.querySelector('#delete-confirm').click()")
        wait_for(cdp, "!document.querySelector('#delete-dialog').open && !document.querySelector('#refresh-files').disabled")
        assert not (fixture / 'שלום folder').exists()
        assert sentinel.read_text() == 'keep outside\n'
        checks.append('Typed folder confirmation deletes nested hidden contents without following symlinks')
        delete('keep.txt')
        (fixture / 'keep.txt').write_text('changed while confirming\n')
        cdp.evaluate("document.querySelector('#delete-confirm').click()")
        wait_for(cdp, "document.querySelector('#delete-status').textContent.includes('changed after')")
        assert (fixture / 'keep.txt').read_text() == 'changed while confirming\n'
        cdp.evaluate("document.querySelector('#delete-cancel').click()")
        checks.append('Stale delete confirmation preserves the changed file')
        cdp.evaluate("document.querySelector('#show-hidden').click()")
        wait_for(cdp, "!document.querySelector('#refresh-files').disabled")
        create('.hidden-new', 'file')
        wait_for(cdp, "document.querySelector('#editor-dialog').open && !document.querySelector('#editor-content').disabled")
        cdp.evaluate("document.querySelector('#editor-cancel').click()")
        assert (fixture / '.hidden-new').is_file()
        assert cdp.evaluate("!document.querySelector('#file-rows').textContent.includes('.hidden-new') && document.querySelector('#file-operation-status').textContent.includes('Show hidden files')")
        assert cdp.evaluate("!document.querySelector('#file-rows tr:first-child .delete-button')"), 'Parent row can be deleted'
        checks.append('Hidden creation respects the filter and parent navigation has no Delete action')
        cdp.call('Emulation.setDeviceMetricsOverride', {'width': 390, 'height': 844, 'deviceScaleFactor': 1, 'mobile': True})
        cdp.evaluate("document.querySelector('#new-folder').click()")
        assert cdp.evaluate("document.querySelector('#create-dialog').getBoundingClientRect().right <= innerWidth && document.documentElement.scrollWidth <= innerWidth")
        cdp.evaluate("document.querySelector('#create-cancel').click()")
        delete('keep.txt')
        assert cdp.evaluate("document.querySelector('#delete-dialog').getBoundingClientRect().right <= innerWidth")
        cdp.evaluate("document.querySelector('#delete-cancel').click()")
        assert not cdp.exceptions, repr(cdp.exceptions)
        checks.append('Create/delete dialogs fit a 390px viewport')
        Path('build/manage-check.json').write_text(json.dumps({'checks': checks}, indent=2) + '\n')
        print(str(len(checks)) + ' file-management browser checks passed')
        for check in checks:
            print('  ' + check)
        cdp.call('Page.navigate', {'url': 'about:blank'})
    finally:
        shutil.rmtree(str(fixture))
        shutil.rmtree(str(outside))


if __name__ == '__main__':
    main()
