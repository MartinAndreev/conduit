import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';

const source = readFileSync(new URL('../assets/bridge.js', import.meta.url), 'utf8');
const catalog = JSON.parse(readFileSync(new URL('../../../i18n/en.json', import.meta.url), 'utf8'));
function environment(app) {
  const nodes = new Map();
  const node = () => ({ hidden: false, textContent: '', dataset: {}, append() {}, replaceChildren() {}, setAttribute() {} });
  const get = key => {
    if (!nodes.has(key)) nodes.set(key, node());
    return nodes.get(key);
  };
  get('#translations').textContent = JSON.stringify(catalog);
  let listener;
  const document = { querySelector: get, querySelectorAll: () => [], createElement: node, getElementById: id => get('#' + id) };
  const window = { go: { main: { App: app } }, runtime: { EventsOn: (_, callback) => { listener = callback; } } };
  vm.runInNewContext(source, { document, window });
  return { get, event: state => listener(state) };
}
const settled = async () => { await new Promise(resolve => setImmediate(resolve)); };
const state = overrides => ({ Generation: 1, Version: 1, Status: 'running', Active: true, ...overrides });

test('failed native actions and snapshot reads show a safe visible error', async () => {
  const reject = async () => { throw new Error('secret raw cause'); };
  const env = environment({ Snapshot: reject, Start: reject, Retry: reject });
  await settled();
  assert.equal(env.get('#transport-error').hidden, false);
  assert.equal(env.get('#transport-error').textContent, catalog['transport.unavailable']);
  assert.ok(!env.get('#transport-error').textContent.includes('secret'));
  await env.get('#retry').onclick();
  assert.equal(env.get('#transport-error').hidden, false);
});

test('late events cannot replace current state and cleanup completion restores controls', async () => {
  const current = state({ Version: 3 });
  const env = environment({ Snapshot: async () => current, Start: async () => current });
  await settled();
  env.event(state({ Version: 4, Status: 'cancelled', Active: false, PendingCleanup: true, Closing: true }));
  assert.equal(env.get('#exit-now').hidden, false);
  assert.equal(env.get('#retry').disabled, true);
  env.event(state({ Version: 5, Status: 'cancelled', Active: false, PendingCleanup: false, Closing: false, CanRetry: true }));
  assert.equal(env.get('#exit-now').hidden, true);
  assert.equal(env.get('#exit').hidden, false);
  assert.equal(env.get('#retry').disabled, false);
  env.event(state({ Version: 2, Status: 'ready', Active: false }));
  env.event(state({ Generation: 0, Version: 100, Status: 'ready', Active: false }));
  assert.equal(env.get('#welcome').hidden, true);
});
