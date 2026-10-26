'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const text = (id, value) => { $(id).textContent = value; };
  const available = value => typeof value === 'number' && Number.isFinite(value);
  function bytes(value) {
    if (!available(value)) return 'Unavailable';
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let n = Math.max(0, value), i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    return `${n.toFixed(i && n < 100 ? 1 : 0)} ${units[i]}`;
  }
  const percent = n => available(n) ? `${n.toFixed(1)}%` : 'Unavailable';
  const temperature = n => available(n) ? `${n.toFixed(1)} °C` : 'Unavailable';
  function value(id, n, unit) {
    const node = $(id); node.replaceChildren();
    if (!available(n)) { const small = document.createElement('small'); small.textContent = 'Unavailable'; node.append(small); return; }
    node.append(document.createTextNode(n.toFixed(1)));
    const suffix = document.createElement('small'); suffix.textContent = unit; node.append(suffix);
  }
  function meter(node, n) {
    node.firstElementChild.style.width = available(n) ? `${Math.min(100, Math.max(0, n))}%` : '0%';
    if (available(n)) { node.setAttribute('aria-valuenow', n.toFixed(1)); node.removeAttribute('aria-valuetext'); }
    else { node.removeAttribute('aria-valuenow'); node.setAttribute('aria-valuetext', 'Unavailable'); }
  }
  function el(tag, className, content) { const n = document.createElement(tag); if (className) n.className = className; if (content !== undefined) n.textContent = content; return n; }
  async function json(url, signal, options = {}) {
    const response = await fetch(url, { signal, cache: 'no-store', credentials: 'same-origin', ...options });
    if (response.status === 401) throw new Error('Sign-in required. Reload the page to sign in again.');
    const body = await response.json();
    if (!response.ok) throw new Error(body.error ? body.error.message : 'The device could not complete this request.');
    return body;
  }
  let lastSample = 0, lastSuccess = 0, pollTimer, pollController, pollInFlight = false;
  function connection(label, stale) { text('connection-status', label); $('connection-status').classList.toggle('stale', stale); }
  function updateAge() {
    if (!lastSample) return;
    const age = Math.max(0, Math.floor((Date.now() - lastSample) / 1000));
    text('last-update', age < 2 ? 'Updated just now' : `Last reading ${age}s ago`);
    if (age > 6 || (lastSuccess && Date.now() - lastSuccess > 6000)) connection('Readings stale', true);
  }
  function renderSystem(s) {
    const identity = s.identity || {};
    text('device-subtitle', `${identity.model || 'Jetson device'} · ${identity.architecture || 'ARM64'} · ${identity.hostname || 'device'}`);
    value('cpu-value', s.cpu_percent, '%'); value('gpu-value', s.gpu_percent, '%');
    meter($('cpu-meter'), s.cpu_percent); meter($('gpu-meter'), s.gpu_percent);
    const ramPercent = available(s.ram_used_bytes) && s.ram_total_bytes > 0 ? 100 * s.ram_used_bytes / s.ram_total_bytes : null;
    value('ram-value', available(s.ram_used_bytes) ? s.ram_used_bytes / 1073741824 : null, 'GiB');
    meter($('ram-meter'), ramPercent);
    text('ram-detail', available(s.ram_used_bytes) ? `${bytes(s.ram_used_bytes)} of ${bytes(s.ram_total_bytes)} · shared RAM` : 'Shared RAM usage unavailable');
    value('temp-value', s.cpu_temp_c, '°C'); text('gpu-temp', `GPU ${temperature(s.gpu_temp_c)}`);
    ['hostname', 'os', 'kernel', 'l4t'].forEach(id => text(id, identity[id] || 'Unavailable'));
    const uptime = s.uptime_seconds;
    text('uptime', available(uptime) ? `${Math.floor(uptime / 86400)}d ${Math.floor(uptime % 86400 / 3600)}h ${Math.floor(uptime % 3600 / 60)}m` : 'Unavailable');
    text('swap', available(s.swap_used_bytes) ? `${bytes(s.swap_used_bytes)} / ${bytes(s.swap_total_bytes)}` : 'Unavailable');
    const storage = $('storage-list'); storage.replaceChildren();
    (s.disks || []).forEach(d => {
      const card = el('div', 'disk'), top = el('div', 'disk-top'), p = d.total_bytes > 0 ? 100 * d.used_bytes / d.total_bytes : null;
      top.append(el('span', 'disk-name', d.mount_path), el('span', 'disk-percent', percent(p)));
      const bar = el('div', 'meter'); bar.append(el('span')); meter(bar, p);
      card.append(top, bar, el('p', 'disk-note', `${bytes(d.used_bytes)} used / ${bytes(d.total_bytes)} · ${bytes(d.available_bytes)} available`)); storage.append(card);
    });
    if (!storage.children.length) storage.append(el('p', 'muted', 'Storage readings unavailable.'));
    const networks = $('network-list'); networks.replaceChildren();
    (s.networks || []).forEach(n => {
      const card = el('div', 'network'), name = el('div'); name.append(el('p', 'network-name', n.name), el('p', 'network-address', n.addresses.length ? n.addresses.join(' · ') : 'No interface address'));
      const rx = el('div', 'network-rate rx'), tx = el('div', 'network-rate');
      rx.append(el('small', '', '↓ RECEIVE'), document.createTextNode(available(n.rx_bytes_per_second) ? `${bytes(n.rx_bytes_per_second)}/s` : 'Unavailable'));
      tx.append(el('small', '', '↑ SEND'), document.createTextNode(available(n.tx_bytes_per_second) ? `${bytes(n.tx_bytes_per_second)}/s` : 'Unavailable'));
      card.append(name, rx, tx); networks.append(card);
    });
    if (!networks.children.length) networks.append(el('p', 'muted', 'Network readings unavailable.'));
    const warnings = s.warnings || []; text('metric-warnings', warnings.join(' · ')); $('metric-warnings').hidden = !warnings.length;
    lastSample = Date.parse(s.sampled_at) || 0; lastSuccess = Date.now();
    connection(s.stale || Date.now() - lastSample > 6000 ? 'Readings stale' : 'Live connection', !!s.stale || Date.now() - lastSample > 6000); updateAge();
  }
  async function poll() {
    if (document.hidden || pollInFlight) return;
    pollInFlight = true; pollController = new AbortController();
    const timeout = setTimeout(() => pollController.abort(), 5000);
    try { renderSystem(await json('/api/system', pollController.signal)); }
    catch (error) { connection(error.name === 'AbortError' ? 'Connection timed out' : 'Device unavailable', true); text('last-update', error.name === 'AbortError' ? 'Retrying automatically…' : error.message); }
    finally { clearTimeout(timeout); pollInFlight = false; if (!document.hidden) pollTimer = setTimeout(poll, 2000); }
  }
  document.addEventListener('visibilitychange', () => { clearTimeout(pollTimer); if (document.hidden) { if (pollInFlight) pollController.abort(); } else poll(); });
  setInterval(updateAge, 1000);

  let currentPath = '', fileController, fileGeneration = 0;
  const fileURL = (route, path) => `${route}?${new URLSearchParams({ path })}`;
  function crumbs(path) {
    const nav = $('breadcrumbs'); nav.replaceChildren();
    const parts = path ? path.split('/') : [];
    const add = (label, target, current) => { const button = el('button', '', label); button.type = 'button'; button.addEventListener('click', () => loadFiles(target)); if (current) button.setAttribute('aria-current', 'page'); nav.append(button); };
    add('Shared files', '', !parts.length);
    parts.forEach((part, i) => { nav.append(el('span', 'crumb-separator', '/')); add(part, parts.slice(0, i + 1).join('/'), i === parts.length - 1); });
  }
  async function loadFiles(path) {
    const generation = ++fileGeneration;
    if (fileController) fileController.abort(); fileController = new AbortController();
    const controller = fileController, timeout = setTimeout(() => controller.abort(), 10000);
    $('file-table').hidden = true; $('file-status').hidden = false; $('file-status').className = 'file-status'; text('file-status', 'Loading shared files…'); text('file-count', '—');
    ['refresh-files', 'show-hidden', 'new-file', 'new-folder'].forEach(id => $(id).disabled = true);
    try {
      const list = await json(fileURL('/api/files', path), controller.signal);
      if (generation !== fileGeneration) return;
      currentPath = list.path; crumbs(currentPath);
      const rows = $('file-rows'); rows.replaceChildren();
      function row(name, kind, size, modified, target, isParent = false) {
        const tr = el('tr'), nameCell = el('td'), nameWrap = el('span', 'file-name');
        const icon = el('span', `file-icon${kind === 'directory' ? ' folder' : ''}`, kind === 'directory' ? '▱' : '≡'); icon.setAttribute('aria-hidden', 'true'); nameWrap.append(icon);
        if (kind === 'directory') { const button = el('button', 'directory-button', name); button.type = 'button'; button.addEventListener('click', () => loadFiles(target)); nameWrap.append(button); }
        else nameWrap.append(el('span', '', name));
        nameCell.append(nameWrap); const action = el('td'), actions = el('div', 'row-actions');
        if (kind === 'file') {
          const link = el('a', 'download-link', '↓ Download'); link.href = fileURL('/api/download', target); link.setAttribute('download', name); link.setAttribute('aria-label', `Download ${name}`);
          const edit = el('button', 'edit-button', 'Edit'); edit.type = 'button'; edit.setAttribute('aria-label', `Edit ${name}`); edit.addEventListener('click', () => openEditor(target));
          const external = el('button', 'external-button', 'External edit'); external.type = 'button'; external.setAttribute('aria-label', `External edit ${name}`); external.addEventListener('click', () => openExternal(target));
          actions.append(link, edit, external);
        }
        else if (kind === 'unavailable') actions.append(el('span', 'muted', 'Unavailable'));
        if (!isParent) {
          const remove = el('button', 'delete-button danger-action', 'Delete'); remove.type = 'button'; remove.setAttribute('aria-label', `Delete ${name}`); remove.addEventListener('click', () => openDelete(target)); actions.append(remove);
        }
        action.append(actions);
        tr.append(nameCell, el('td', '', kind === 'file' ? bytes(size) : '—'), el('td', '', modified ? new Date(modified).toLocaleString([], { year:'numeric', month:'short', day:'numeric', hour:'2-digit', minute:'2-digit' }) : '—'), action); rows.append(tr);
      }
      if (list.parent !== null) row('.. Parent directory', 'directory', null, null, list.parent, true);
      const entries = list.entries.filter(entry => $('show-hidden').checked || !entry.name.startsWith('.'));
      entries.forEach(entry => row(entry.name, entry.kind, entry.size_bytes, entry.modified_at, currentPath ? `${currentPath}/${entry.name}` : entry.name));
      text('file-count', `${entries.length}${list.truncated ? '+' : ''} items`);
      $('file-table').hidden = !rows.children.length;
      $('file-status').hidden = entries.length > 0 && !list.truncated;
      text('file-status', list.truncated ? 'Showing part of this folder (1,000-entry scan limit). Use smaller subfolders to see all files.' : list.entries.length && !entries.length ? 'Only hidden items are in this folder. Select “Show hidden files” to see them.' : 'This folder is empty.');
    } catch (error) {
      if (generation !== fileGeneration) return;
      $('file-status').className = 'file-status error'; text('file-status', error.name === 'AbortError' ? 'The request timed out. Refresh to try again.' : error.message);
    } finally { clearTimeout(timeout); if (generation === fileGeneration) ['refresh-files', 'show-hidden', 'new-file', 'new-folder'].forEach(id => $(id).disabled = false); }
  }
  $('refresh-files').addEventListener('click', () => loadFiles(currentPath));
  $('show-hidden').addEventListener('change', () => loadFiles(currentPath));

  function operationStatus(message) { text('file-operation-status', message); $('file-operation-status').hidden = !message; }
  async function changeEntry(path, method, payload) {
    const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 30000);
    try { return await json(fileURL('/api/entry', path), controller.signal, { method, headers: { 'Content-Type': 'application/json', 'X-Dashboard-Edit': '1' }, body: JSON.stringify(payload) }); }
    catch (error) { if (error.name === 'AbortError') throw new Error('The response timed out. The operation may still finish; refresh the folder before retrying.'); throw error; }
    finally { clearTimeout(timeout); }
  }
  const createDialog = $('create-dialog');
  let createKind = 'file', createParent = '', creating = false;
  function openCreate(kind) {
    createKind = kind; createParent = currentPath; $('create-name').value = ''; text('create-status', '');
    text('create-title', kind === 'directory' ? 'New folder' : 'New file'); text('create-location', `In: ${createParent || 'Shared files'}`);
    createDialog.showModal(); $('create-name').focus();
  }
  $('new-file').addEventListener('click', () => openCreate('file'));
  $('new-folder').addEventListener('click', () => openCreate('directory'));
  $('create-cancel').addEventListener('click', () => { if (!creating) createDialog.close(); });
  createDialog.addEventListener('cancel', event => { if (creating) event.preventDefault(); });
  $('create-form').addEventListener('submit', async event => {
    event.preventDefault(); if (creating) return;
    const name = $('create-name').value;
    if (name.includes('/') || name === '.' || name === '..') { text('create-status', 'Enter a single name without a slash, dot, or dot-dot path.'); return; }
    const path = createParent ? `${createParent}/${name}` : name;
    creating = true; ['create-submit', 'create-cancel', 'create-name'].forEach(id => $(id).disabled = true); text('create-status', 'Creating…');
    try {
      const entry = await changeEntry(path, 'POST', { kind: createKind });
      createDialog.close(); await loadFiles(createParent);
      operationStatus(`Created “${entry.name}”.${entry.name.startsWith('.') && !$('show-hidden').checked ? ' Select “Show hidden files” to see it in the list.' : ''}`);
      if (entry.kind === 'file') openEditor(entry.path);
    } catch (error) { text('create-status', error.message); }
    finally { creating = false; ['create-submit', 'create-cancel', 'create-name'].forEach(id => $(id).disabled = false); }
  });

  const deleteDialog = $('delete-dialog');
  let deletePreview = null, deleting = false, deleteController;
  const updateDelete = () => { $('delete-confirm').disabled = deleting || !deletePreview || (deletePreview.kind === 'directory' && $('delete-name').value !== deletePreview.name); };
  async function openDelete(path) {
    deletePreview = null; $('delete-name').value = ''; $('delete-name').hidden = true; $('delete-name-label').hidden = true;
    text('delete-path', path); text('delete-description', ''); text('delete-status', 'Loading entry…'); updateDelete(); deleteDialog.showModal();
    deleteController = new AbortController();
    const controller = deleteController, timeout = setTimeout(() => controller.abort(), 10000);
    try {
      const entry = await json(fileURL('/api/entry', path), controller.signal);
      if (!deleteDialog.open || controller !== deleteController) return;
      deletePreview = entry;
      const folder = entry.kind === 'directory';
      text('delete-description', folder ? `Permanently delete “${entry.name}” and everything inside it, including hidden files? This cannot be undone.` : entry.kind === 'symlink' ? `Permanently remove the link “${entry.name}”? Its target will be kept.` : `Permanently delete “${entry.name}”? This cannot be undone.`);
      $('delete-name').hidden = !folder; $('delete-name-label').hidden = !folder;
      text('delete-name-label', `Type “${entry.name}” to confirm`); text('delete-status', ''); updateDelete();
      if (folder) $('delete-name').focus();
    } catch (error) { if (deleteDialog.open && controller === deleteController) text('delete-status', error.name === 'AbortError' ? 'Loading timed out. Cancel and try again.' : error.message); }
    finally { clearTimeout(timeout); }
  }
  $('delete-name').addEventListener('input', updateDelete);
  $('delete-cancel').addEventListener('click', () => { if (!deleting) deleteDialog.close(); });
  deleteDialog.addEventListener('cancel', event => { if (deleting) event.preventDefault(); });
  deleteDialog.addEventListener('close', () => { if (deleteController) deleteController.abort(); });
  $('delete-confirm').addEventListener('click', async () => {
    if ($('delete-confirm').disabled) return;
    const entry = deletePreview;
    deleting = true; updateDelete(); $('delete-cancel').disabled = true; $('delete-name').disabled = true; text('delete-status', 'Deleting…');
    try {
      await changeEntry(entry.path, 'DELETE', { revision: entry.revision, name: entry.kind === 'directory' ? $('delete-name').value : entry.name });
      deleteDialog.close(); await loadFiles(currentPath); operationStatus(`Deleted “${entry.name}”.`);
    } catch (error) { deletePreview = null; text('delete-status', error.message); loadFiles(currentPath); }
    finally { deleting = false; $('delete-cancel').disabled = false; $('delete-name').disabled = false; updateDelete(); }
  });

  const editor = $('editor-dialog'), editorContent = $('editor-content');
  let editingPath = '', editingRevision = '', originalText = '', saving = false, editorController;
  const dirty = () => editor.open && !!editingRevision && editorContent.value !== originalText;
  const updateSave = () => { $('editor-save').disabled = saving || !dirty(); };
  async function openEditor(path) {
    editingPath = path; editingRevision = ''; originalText = ''; editorContent.value = ''; editorContent.disabled = true;
    text('editor-path', path); text('editor-status', 'Loading…'); updateSave(); editor.showModal();
    editorController = new AbortController();
    const controller = editorController, timeout = setTimeout(() => controller.abort(), 10000);
    try {
      const doc = await json(fileURL('/api/edit', path), controller.signal);
      if (!editor.open || controller !== editorController) return;
      editingRevision = doc.revision; editorContent.value = doc.content; originalText = editorContent.value;
      editorContent.disabled = false; text('editor-status', 'UTF-8 text · up to 1 MiB'); editorContent.focus();
    } catch (error) {
      if (editor.open && controller === editorController) text('editor-status', error.name === 'AbortError' ? 'Loading timed out. Close and try again.' : error.message);
    } finally { clearTimeout(timeout); }
  }
  function closeEditor() {
    if (saving || (dirty() && !window.confirm('Discard your unsaved changes?'))) return;
    if (editorController) editorController.abort();
    editor.close();
  }
  editorContent.addEventListener('input', () => { updateSave(); if (dirty()) text('editor-status', 'Unsaved changes'); });
  editor.addEventListener('cancel', event => { event.preventDefault(); closeEditor(); });
  $('editor-cancel').addEventListener('click', closeEditor);
  window.addEventListener('beforeunload', event => { if (dirty()) { event.preventDefault(); event.returnValue = ''; } });
  $('editor-save').addEventListener('click', async () => {
    if (saving || !dirty()) return;
    saving = true; updateSave(); editorContent.disabled = true; $('editor-cancel').disabled = true; text('editor-status', 'Saving…');
    const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 15000);
    try {
      const saved = await json(fileURL('/api/edit', editingPath), controller.signal, { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-Dashboard-Edit': '1' }, body: JSON.stringify({ content: editorContent.value, revision: editingRevision }) });
      editingRevision = saved.revision; originalText = editorContent.value; text('editor-status', 'Saved.');
      loadFiles(currentPath);
    } catch (error) { text('editor-status', error.name === 'AbortError' ? 'Save response timed out. Your edits are kept here; check the file before retrying.' : error.message); }
    finally { clearTimeout(timeout); saving = false; editorContent.disabled = false; $('editor-cancel').disabled = false; updateSave(); }
  });

  const externalDialog = $('external-dialog');
  let externalController;
  async function openExternal(path) {
    $('external-path').value = ''; $('external-url').value = ''; $('external-copy').disabled = true; $('external-open').hidden = true; $('external-open').removeAttribute('href');
    text('external-connection', ''); text('external-status', 'Loading connection details…'); externalDialog.showModal();
    externalController = new AbortController();
    const controller = externalController, timeout = setTimeout(() => controller.abort(), 10000);
    try {
      const details = await json(fileURL('/api/external', path), controller.signal);
      if (!externalDialog.open || controller !== externalController) return;
      $('external-path').value = details.path; $('external-url').value = details.url;
      text('external-connection', `Host: ${details.host} · Port: ${details.port} · SSH user: ${details.username}`);
      $('external-open').href = details.url; $('external-open').hidden = false; $('external-copy').disabled = false; text('external-status', '');
    } catch (error) { if (externalDialog.open && controller === externalController) text('external-status', error.name === 'AbortError' ? 'Loading timed out. Close and try again.' : error.message); }
    finally { clearTimeout(timeout); }
  }
  $('external-close').addEventListener('click', () => externalDialog.close());
  externalDialog.addEventListener('close', () => { if (externalController) externalController.abort(); });
  ['external-path', 'external-url'].forEach(id => $(id).addEventListener('focus', () => $(id).select()));
  $('external-copy').addEventListener('click', async () => {
    try { await navigator.clipboard.writeText($('external-url').value); text('external-status', 'Link copied.'); }
    catch (error) { $('external-url').focus(); $('external-url').select(); text('external-status', 'Select and copy the SFTP link above.'); }
  });
  poll(); loadFiles('');
})();
