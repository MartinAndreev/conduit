'use strict';
// Transport-only renderer. All effects and readiness decisions are Go-owned.
const copy = JSON.parse(document.querySelector('#translations').textContent);
const text = key => copy[key] || key;
const format = (value, ...args) => args.reduce(
  (result, arg) => result.replace(/%[ds]/, String(arg)), value
);
for (const node of document.querySelectorAll('[data-copy]')) {
  node.textContent = text(node.dataset.copy);
}
document.querySelector('.settings').title = text('unavailable.settings');
let generation = 0;
let version = 0;
const pendingSteps = ['check_configuration', 'discover_harnesses', 'create_settings', 'check_readiness'];

function diagnostics(target, items) {
  target.replaceChildren();
  for (const diagnostic of items || []) {
    const node = document.createElement('p');
    node.className = 'diagnostic';
    const key = 'diagnostic.' + diagnostic.Code;
    const label = copy[key] || format(text('diagnostic.unknown'), diagnostic.Code);
    node.textContent = (diagnostic.Harness ? diagnostic.Harness + ': ' : '') + label;
    target.append(node);
  }
}

function renderSteps(steps) {
  const list = document.querySelector('#steps');
  list.replaceChildren();
  let complete = 0;
  for (const step of steps) {
    const row = document.createElement('li');
    row.className = step.Status;
    const mark = document.createElement('span');
    mark.className = 'mark';
    mark.setAttribute('aria-hidden', 'true');
    if (step.Status === 'completed' || step.Status === 'skipped') {
      mark.textContent = '✓';
      complete++;
    } else if (step.Status === 'failed') {
      mark.textContent = '!';
    }
    const label = document.createElement('span');
    label.textContent = text('step.' + step.ID);
    const detail = document.createElement('div');
    detail.className = 'details';
    detail.textContent = text('status.' + step.Status);
    label.append(detail);
    row.append(mark, label);
    list.append(row);
  }
  document.querySelector('#progress').setAttribute('aria-valuenow', String(complete));
  document.querySelector('#count').textContent = format(text('startup.steps'), complete, 4);
}

function render(state) {
  if (state.Generation < generation ||
      (state.Generation === generation && state.Version < version)) return;
  generation = state.Generation;
  version = state.Version;
  const welcome = state.Status === 'ready' || state.Status === 'ready_with_warnings';
  document.querySelector('#startup').hidden = welcome;
  document.querySelector('#welcome').hidden = !welcome;
  document.querySelector('#location').textContent = text(welcome ? 'welcome.title' : 'startup.title');
  const title = state.Status === 'blocked' ? 'blocked' : state.Status === 'cancelled' ? 'cancelled' : 'startup';
  document.querySelector('#heading').textContent = text('title.' + title);
  renderSteps(state.Steps || pendingSteps.map(ID => ({ ID, Status: 'pending' })));
  document.querySelector('#readiness').textContent = text('startup.access_unknown');
  document.querySelector('#config').textContent = state.ConfigPath
    ? format(text('label.configuration'), state.ConfigPath) : '';
  diagnostics(document.querySelector('#diagnostics'), state.Diagnostics);
  diagnostics(document.querySelector('#welcome-diagnostics'), state.Diagnostics);
  document.querySelector('#cancel').hidden = !state.Active;
  document.querySelector('#retry').hidden = state.Active;
  document.querySelector('#retry').disabled = !state.CanRetry;
  const uncertain = state.PendingCleanup || state.Closing;
  document.querySelector('#cleanup').hidden = !uncertain;
  document.querySelector('#cleanup').textContent = text(state.PendingCleanup ? 'cleanup.pending' : 'cleanup.active');
  document.querySelector('#wait').hidden = !uncertain;
  document.querySelector('#exit-now').hidden = !uncertain;
  document.querySelector('#exit').hidden = state.Active || uncertain;
}

function transportFailure() {
  document.querySelector('#transport-error').hidden = false;
  document.querySelector('#transport-error').textContent = text('transport.unavailable');
}

async function refresh() {
  try {
    render(await window.go.main.App.Snapshot());
    document.querySelector('#transport-error').hidden = true;
  } catch (_) {
    transportFailure();
  }
}

async function invoke(method) {
  try {
    const result = await window.go.main.App[method]();
    if (result && typeof result === 'object') render(result);
    await refresh();
  } catch (_) {
    // A rejected action may leave valid current state; heal once, then show a
    // safe connection error if the authoritative read also fails.
    await refresh();
  }
}

for (const [id, method] of [
  ['cancel', 'Cancel'], ['retry', 'Retry'], ['wait', 'Wait'],
  ['exit', 'Exit'], ['exit-now', 'ExitNow']
]) {
  document.getElementById(id).onclick = () => invoke(method);
}

// Subscribe before Start. Reload cannot start a second operation; Snapshot
// heals missed events. A missing native bridge produces visible safe feedback.
async function connect() {
  try {
    window.runtime.EventsOn('startup:state', render);
    await refresh();
    await invoke('Start');
  } catch (_) {
    transportFailure();
  }
}
connect();
