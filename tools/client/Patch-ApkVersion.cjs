// Update only versionName/versionCode in a binary AndroidManifest.xml.
// Equal-length version names preserve the string pool and all resource offsets.
const fs = require('node:fs');
const assert = require('node:assert/strict');
const [input, output, oldName, newName, oldCodeText, newCodeText] = process.argv.slice(2);
assert(input && output && oldName && newName && oldCodeText && newCodeText,
  'Usage: node Patch-ApkVersion.cjs input output oldName newName oldCode newCode');
assert(!fs.existsSync(output), 'Output must be a new file');
const oldCode = Number(oldCodeText), newCode = Number(newCodeText);
assert(Number.isSafeInteger(oldCode) && Number.isSafeInteger(newCode) && newCode > oldCode && newCode <= 0x7fffffff);
const original = fs.readFileSync(input), data = Buffer.from(original);
assert.equal(data.readUInt16LE(0), 3, 'Expected binary Android XML');
assert.equal(data.readUInt32LE(4), data.length);
const strings = [], locations = [], changes = [];
let resources = [];
function length16(at) {
  const first = data.readUInt16LE(at);
  return first & 0x8000 ? [((first & 0x7fff) << 16) | data.readUInt16LE(at + 2), at + 4] : [first, at + 2];
}
function length8(at) {
  const first = data[at];
  return first & 0x80 ? [((first & 0x7f) << 8) | data[at + 1], at + 2] : [first, at + 1];
}
for (let p = data.readUInt16LE(2); p < data.length;) {
  const type = data.readUInt16LE(p), header = data.readUInt16LE(p + 2), size = data.readUInt32LE(p + 4);
  assert(size >= header && header >= 8 && p + size <= data.length);
  if (type === 1) {
    const count = data.readUInt32LE(p + 8), utf8 = !!(data.readUInt32LE(p + 16) & 0x100);
    const start = p + data.readUInt32LE(p + 20);
    for (let i = 0; i < count; i++) {
      let at = start + data.readUInt32LE(p + header + i * 4), length;
      if (utf8) {
        [, at] = length8(at);
        [length, at] = length8(at);
      } else {
        [length, at] = length16(at);
        length *= 2;
      }
      const encoding = utf8 ? 'utf8' : 'utf16le';
      strings.push(data.toString(encoding, at, at + length));
      locations.push({at, length, encoding});
    }
  } else if (type === 0x180) {
    resources = Array.from({length: (size - header) / 4}, (_, i) => data.readUInt32LE(p + header + i * 4));
  } else if (type === 0x102 && strings[data.readUInt32LE(p + 20)] === 'manifest') {
    const start = p + 16 + data.readUInt16LE(p + 24), stride = data.readUInt16LE(p + 26), count = data.readUInt16LE(p + 28);
    assert(stride >= 20 && start + stride * count <= p + size);
    for (let i = 0; i < count; i++) {
      const at = start + i * stride, nameIndex = data.readUInt32LE(at + 4), resource = resources[nameIndex];
      if (resource === 0x0101021b) {
        assert.equal(strings[nameIndex], 'versionCode');
        assert.equal(data[at + 15], 0x10, 'versionCode must be an integer');
        assert.equal(data.readUInt32LE(at + 16), oldCode);
        data.writeUInt32LE(newCode, at + 16);
        changes.push('versionCode');
      } else if (resource === 0x0101021c) {
        assert.equal(strings[nameIndex], 'versionName');
        assert.equal(data[at + 15], 3, 'versionName must be a string');
        const index = data.readUInt32LE(at + 16), location = locations[index];
        assert.equal(strings[index], oldName);
        const replacement = Buffer.from(newName, location.encoding);
        assert.equal(replacement.length, location.length, 'Version names must have equal encoded lengths');
        replacement.copy(data, location.at);
        changes.push('versionName');
      }
    }
  }
  p += size;
}
assert.deepEqual(changes.sort(), ['versionCode', 'versionName']);
assert.equal(data.length, original.length);
fs.writeFileSync(output, data, {flag: 'wx'});
console.log(`PASS: versionName ${oldName} -> ${newName}; versionCode ${oldCode} -> ${newCode}`);
