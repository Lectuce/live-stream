#!/usr/bin/env python3
"""L4-D acceptance. Use TWO DEDICATED test accounts: this clears their history."""
import argparse
import runpy
import uuid
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import urlencode

Client = runpy.run_path(str(Path(__file__).with_name('test-content-comments.py')))['Client']
ROOT = '/api/v1/me/history'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    parser.add_argument('--video-id', required=True, type=int)
    parser.add_argument('--other-video-id', required=True, type=int)
    parser.add_argument('--unavailable-video-id', type=int, help='Optional existing draft/unpublished video; never modifies it')
    parser.add_argument('--allow-clear-history', action='store_true', help='Explicitly permit clearing BOTH test accounts\' history')
    args = parser.parse_args()
    if not args.allow_clear_history:
        parser.error('Use dedicated test accounts and pass --allow-clear-history; both accounts\' history will be cleared.')
    assert args.video_id != args.other_video_id, '需要两个不同的公开视频'
    a, b, guest = [Client(args.base_url) for _ in range(3)]
    ua, ub = a.login('测试账号 A '), b.login('测试账号 B ')
    assert ua['user_id'] != ub['user_id'], '请使用不同账号'
    for video in [args.video_id, args.other_video_id]:
        data = guest.call(f'/api/v1/videos/{video}')['data']
        assert data['duration_ms'] > 10000, '测试视频需要长于10秒'

    def path(video=args.video_id):
        return f'{ROOT}/{video}'

    def claim(client, video=args.video_id):
        old = client.call(path(video))['data']
        body = {k: old[k] for k in ['history_epoch', 'session_version']}
        body['session_id'] = str(uuid.uuid4())
        session = client.call(path(video) + '/session', 'POST', body)['data']
        return session, body

    def payload(session, seq, position):
        return dict({k: session[k] for k in ['session_id', 'session_version', 'history_epoch']}, seq=seq, position_ms=position)

    def save(client, session, seq, position, expected=200, video=args.video_id):
        return client.call(path(video) + '/progress', 'PUT', payload(session, seq, position), expected=expected)

    guest.call(ROOT, expected=401)
    guest.call(path() + '/progress', 'PUT', {}, expected=401)
    for client in [a, b]:
        client.call(ROOT, 'DELETE')
    try:
        assert not a.call(path())['data']['has_history']
        first, request = claim(a)
        assert a.call(ROOT)['data']['items'] == [], '打开/创建会话不能产生可见历史'
        save(a, first, 2, 8000)
        save(a, first, 1, 9000)
        assert a.call(path())['data']['position_ms'] == 8000, '乱序请求覆盖进度'
        save(a, first, 3, 2000)
        assert a.call(path())['data']['position_ms'] == 2000, '主动回退未保存'
        save(a, first, 3, 2000)
        save(a, first, 3, 2500, expected=400)
        retried = a.call(path() + '/session', 'POST', request)['data']
        assert retried['session_version'] == first['session_version'] and retried['last_seq'] == '3'
        second, _ = claim(a)
        save(a, first, 4, 9000, expected=409)
        a.call(path() + '/session', 'POST', request, expected=409)
        save(a, second, 1, 4000)
        # Concurrent arrivals must finish at the newest sequence, in either arrival order.
        with ThreadPoolExecutor(max_workers=2) as pool:
            list(pool.map(lambda pair: save(a, second, *pair), [(3, 3000), (2, 2000)]))
        assert a.call(path())['data']['position_ms'] == 3000
        save(b, second, 5, 6000, expected=409)
        other_user, _ = claim(b)
        save(b, other_user, 1, 1000)
        b.call(path(), 'DELETE')
        assert a.call(path())['data']['position_ms'] == 3000, 'B删除了A的记录'
        save(b, other_user, 2, 2000, expected=409)

        other_video, _ = claim(a, args.other_video_id)
        save(a, other_video, 1, 1000, video=args.other_video_id)
        page = a.call(ROOT + '?limit=1')['data']
        assert page['has_more'] and len(page['items']) == 1
        cursor = page['next_cursor']
        tail = a.call(ROOT + '?' + urlencode({'limit': 1, 'cursor': cursor}))['data']
        assert not tail['has_more'] and len(tail['items']) == 1
        assert page['items'][0]['video_id'] != tail['items'][0]['video_id']
        b.call(ROOT + '?' + urlencode({'cursor': cursor}), expected=400)
        a.call(ROOT + '?cursor=invalid', expected=400)
        a.call(ROOT + '?limit=0', expected=400)
        a.call(path(), 'DELETE')
        save(a, second, 9, 9000, expected=409)
        assert not a.call(path())['data']['has_history']
        a.call(ROOT, 'DELETE')
        save(a, other_video, 2, 2000, expected=409, video=args.other_video_id)
        a.call(ROOT + '?' + urlencode({'cursor': cursor}), expected=409)
        assert a.call(ROOT)['data']['items'] == []
        restarted, _ = claim(a)
        save(a, restarted, 1, 1000)
        assert a.call(path())['data']['has_history'], '主动重新播放无法恢复记录'
        if args.unavailable_video_id:
            a.call(path(args.unavailable_video_id), expected=404)
        print('PASS: 续播、回退、乱序、并发、会话重试/接管、账号隔离、分页、删除/清空及旧请求失效')
    finally:
        for client in [a, b]:
            client.call(ROOT, 'DELETE')


if __name__ == '__main__':
    main()
