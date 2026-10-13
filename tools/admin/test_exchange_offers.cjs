// Run with: node tools/admin/test_exchange_offers.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const source = fs.readFileSync(path.join(__dirname, '../../server/internal/admin/web/admin_content.js'), 'utf8');
const helpers = ['contentKey', 'contentReward'].map(name => source.match(new RegExp(`^const ${name}=.*$`, 'm'))[0]).join('\n');
const start = source.indexOf('function addExchangeOffers(');
const addOffers = source.slice(start, source.indexOf("$('#exchange-add')", start));
const item = { reward_type: 8, reward_type_id: 4001, name: '道具4001' };

function editor() {
  const state = { shop: { lineups: [] }, nextLineup: 62000001, selected: new Set(), dirty: false };
  const defaults = { currency: 4000, price: 100, stock: 99 };
  const messages = [];
  const context = vm.createContext({
    exchangeEditor: state,
    exchangeDefaults: () => defaults,
    changedContent: () => { state.dirty = true; },
    renderExchangeForm: () => {},
    toast: message => messages.push(message),
  });
  vm.runInContext(helpers + '\n' + addOffers, context);
  return { state, defaults, messages, add: rows => context.addExchangeOffers(rows) };
}

test('4001 can be offered for each of 4000, 4002, 4003 and 4006', () => {
  const e = editor();
  const currencies = [4000, 4002, 4003, 4006];
  currencies.forEach((currency, index) => {
    Object.assign(e.defaults, { currency, price: 100 + index, stock: index + 1 });
    e.add([item]);
  });
  assert.equal(e.state.shop.lineups.length, 4);
  e.state.shop.lineups.forEach((offer, index) => {
    assert.equal(offer.rewards[0].reward_typeid, 4001);
    assert.equal(offer.prices.length, 1);
    assert.equal(offer.prices[0].id, currencies[index]);
    assert.equal(offer.prices[0].num, 100 + index);
    assert.equal(offer.stock_num, index + 1);
    assert.equal(offer.lineupid, 62000001 + index);
  });
  assert.equal(e.state.selected.size, 4);
  e.state.dirty = false;
  e.add([item, item]);
  assert.equal(e.state.shop.lineups.length, 4);
  assert.equal(e.state.dirty, false);
  assert.match(e.messages.at(-1), /跳过 2 项使用相同兑换货币/);
});

test('disabled offers keep their identity when a replacement is added', () => {
  const e = editor();
  e.add([item]);
  const old = e.state.shop.lineups[0];
  old.disabled = true;
  e.add([item, item]);
  assert.equal(e.state.shop.lineups.length, 2);
  assert.equal(old.disabled, true);
  assert.notEqual(old.lineupid, e.state.shop.lineups[1].lineupid);
});

test('capacity counts only new offers and rejects an oversized batch atomically', () => {
  const e = editor();
  const rows = Array.from({ length: 500 }, (_, index) => ({ ...item, reward_type_id: 5000 + index }));
  e.add(rows.slice(0, 499));
  e.add(rows);
  assert.equal(e.state.shop.lineups.length, 500);
  e.add(rows);
  assert.equal(e.state.shop.lineups.length, 500);
  e.defaults.currency = 4002;
  e.state.dirty = false;
  const snapshot = JSON.stringify(e.state.shop);
  const nextID = e.state.nextLineup;
  assert.throws(() => e.add(rows), /最多500项/);
  assert.equal(JSON.stringify(e.state.shop), snapshot);
  assert.equal(e.state.nextLineup, nextID);
  assert.equal(e.state.dirty, false);
});
