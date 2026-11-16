#!/usr/bin/env python3
"""Read-only L5 API acceptance. Optional --behavior records real deduplicated test behavior."""
import argparse
import runpy
import time
from pathlib import Path
from urllib.parse import urlencode

Client = runpy.run_path(str(Path(__file__).with_name('test-content-comments.py')))['Client']
FEED = '/api/v1/feeds/videos'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    parser.add_argument('--expect-min', type=int, default=0, help='Use 60 to require a three-page fixture')
    parser.add_argument('--behavior', action='store_true', help='Login with a test account; records impressions/clicks/play (about 35 seconds)')
    args = parser.parse_args()
    client = Client(args.base_url)
    categories = client.call('/api/v1/video-categories')['data']
    assert any(c['id'] == 'general' for c in categories)
    first = None
    for mode in ['latest', 'hot', 'recommended']:
        cursor, seen, page_number, effective = '', set(), 0, mode
        while True:
            query = {'sort': effective, 'cursor': cursor, 'limit': 20}
            page = client.call(FEED + '?' + urlencode(query))['data']
            page_number += 1
            if not cursor and mode == 'latest':
                first = page
            ids = [v['video_id'] for v in page['items']]
            assert len(ids) <= 20 and len(set(ids)) == len(ids)
            assert seen.isdisjoint(ids), f'{mode}: 跨页重复'
            seen.update(ids)
            for item in page['items']:
                assert 'play_url' not in item and 'media_file' not in item
                assert 'user_email' not in item and 'audit_reason' not in item
            if effective == 'latest':
                keys = [(v['published_at'], int(v['video_id'])) for v in page['items']]
                assert keys == sorted(keys, reverse=True), '最新列表顺序错误'
            print(f'{mode} 第{page_number}页: {len(ids)}条, has_more={page["has_more"]}, degraded={page["degraded"]}')
            if cursor:
                retry = client.call(FEED + '?' + urlencode(query))['data']
                assert ids == [v['video_id'] for v in retry['items']], '稳定测试数据下游标重试结果不同'
            if not page['has_more']:
                assert not page['next_cursor']
                break
            assert page['next_cursor'] and page['next_cursor'] != cursor
            cursor = page['next_cursor']
            effective = page['mode']  # Explicit latest fallback keeps its own cursor type.
            if page_number == 1:
                client.call(FEED + '?' + urlencode({'sort': effective, 'category': 'games', 'cursor': cursor}), expected=400)
            assert page_number <= 25, '测试数据过多，请使用独立测试环境'
        assert len(seen) >= args.expect_min, f'{mode}只有{len(seen)}条，请准备足够的公开视频'
    for invalid in ['limit=0', 'limit=51', 'sort=unknown', 'category=private', 'cursor=invalid']:
        client.call(FEED + '?' + invalid, expected=400)
    for category in categories:
        page = client.call(FEED + '?' + urlencode({'sort': 'latest', 'category': category['id']}))['data']
        assert all(v['category'] == category['id'] for v in page['items'])
    for kind in ['video', 'user', 'live']:
        result = client.call('/api/v1/search?' + urlencode({'type': kind, 'q': '测试'}))['data']
        assert len(result['items']) <= 20
        for item in result['items']:
            assert set(item) == {'id', 'kind', 'title', 'image'}, '搜索暴露私有字段'
    if args.behavior:
        assert first and first['items'], '需要至少一个公开视频'
        client.login('测试账号 ')
        item = first['items'][0]
        assert item['duration_ms'] >= 40000, '行为验证需要首个视频超过40秒'
        path = '/api/v1/videos/' + item['video_id']
        token = first['delivery_token']
        assert token, 'Redis不可用时无法验证曝光来源'
        for kind in ['impression', 'click']:
            body = {'kind': kind, 'delivery_token': token}
            client.call(path + '/behavior', 'POST', body)
            client.call(path + '/behavior', 'POST', body)
        client.call(path + '/behavior', 'POST', {'kind': 'play', 'delivery_token': token}, expected=400)
        session = client.call(path + '/play-sessions', 'POST')['data']['session_id']
        pulse = {'session_id': session, 'seq': 1, 'position_ms': 35000}
        assert not client.call(path + '/play-sessions', 'PUT', pulse)['data']['qualified'], '直接拖到末尾不应产生有效播放'
        pulse.update(seq=2, position_ms=0)
        client.call(path + '/play-sessions', 'PUT', pulse)
        for n in range(1, 8):
            time.sleep(5.1)
            pulse.update(seq=n+2, position_ms=n*5000)
            response = client.call(path + '/play-sessions', 'PUT', pulse)['data']
        assert response['qualified'], '连续正常播放未被计入'
        assert client.call(path + '/play-sessions', 'PUT', pulse)['data']['qualified']
        print('PASS: 来源校验、行为重复请求、跳转不计播放、连续有效播放')
    print('PASS: 三种列表分页、游标校验、分区过滤、搜索公开字段')


if __name__ == '__main__':
    main()
