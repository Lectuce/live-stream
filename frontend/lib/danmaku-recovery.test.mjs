import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DanmakuRecovery } from './danmaku-recovery.ts';

const flush = async () => { for(let i=0;i<10;i++) await Promise.resolve(); };
test('merges older hints without skipping unfinished pages', async t => {
 t.mock.timers.enable({apis:['setTimeout']});
 const calls=[]; let resolve;
 const recovery=new DanmakuRecovery(cursor => { calls.push(cursor); return new Promise(r=>{resolve=r}); },()=>{},()=>{},()=>0);
 recovery.request(100,110); t.mock.timers.tick(0);
 recovery.request(90,120);
 resolve({items:[],next_seq:105,has_more:false}); await flush();
 t.mock.timers.tick(3000);
 assert.deepEqual(calls,[100,90]);
 recovery.stop(); resolve({items:[],next_seq:120,has_more:false}); await flush();
});
test('honours Retry-After and cancels future retries', async t => {
 t.mock.timers.enable({apis:['setTimeout']});
 let calls=0;
 const recovery=new DanmakuRecovery(async()=>{calls++;throw {retryAfterMilliseconds:5000};},()=>{},()=>{},()=>0);
 recovery.request(0);t.mock.timers.tick(0);await flush();
 t.mock.timers.tick(4999);assert.equal(calls,1);
 t.mock.timers.tick(1);await flush();assert.equal(calls,2);
 recovery.stop();t.mock.timers.tick(60000);assert.equal(calls,2);
});
