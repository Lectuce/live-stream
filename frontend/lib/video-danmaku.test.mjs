import assert from 'node:assert/strict';
import { timelineRows } from './video-danmaku.ts';
const item = (id, position, content = 'hello') => ({
  danmaku_id: String(id),
  position_ms: position,
  content,
  user_id: '9',
  video_id: '7',
  color: '#ffffff',
  version: '1',
});
const rows = [item(1, 10000), item(2, 10000), item(1, 10000)];
assert.equal(timelineRows(rows, 9999, 4, [], []).length, 0);
assert.equal(timelineRows(rows, 11000, 4, [], []).length, 2);
assert.deepEqual(
  timelineRows(rows, 11000, 4, [], []),
  timelineRows(rows, 11000, 4, [], []),
  'pause must freeze positions',
);
assert.equal(timelineRows(rows, 14000, 4, [], [])[0].progress, 0.5);
assert.equal(timelineRows(rows, 18000, 4, [], []).length, 0);
assert.equal(
  timelineRows(rows, 11000, 4, [], []).length,
  2,
  'rewind must replay',
);
assert.equal(timelineRows(rows, 11000, 4, ['hello'], []).length, 0);
assert.equal(timelineRows(rows, 11000, 4, [], ['9']).length, 0);
const dense = Array.from({ length: 200 }, (_, i) => item(i + 1, 10000));
assert.ok(timelineRows(dense, 11000, 4, [], []).length <= 4);
const boundary = [item(1, 55000), item(5, 56000), item(9, 63000)];
assert.equal(
  timelineRows(boundary, 57000, 4, [], []).length,
  1,
  'same lane must not overlap',
);
assert.deepEqual(
  timelineRows([item(99, 1000), ...boundary], 64000, 4, [], []),
  timelineRows(boundary, 64000, 4, [], []),
  'evicting old windows must not change active lanes',
);
console.log('Video danmaku timeline tests passed');
