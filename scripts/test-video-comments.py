#!/usr/bin/env python3
"""Comment API smoke test against two existing published videos and two test accounts."""
import argparse
import getpass
import http.cookiejar
import json
import urllib.error
import urllib.parse
import urllib.request
import uuid


class Client:
    def __init__(self, base):
        self.base = base.rstrip('/')
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, path, method='GET', body=None, expected=200):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.base + path, data=data, method=method, headers={'Content-Type': 'application/json'})
        try:
            response = self.opener.open(request, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            assert response.status == expected, f'{method} {path}: expected {expected}, got {response.status}: {raw[:300]!r}'
            return json.loads(raw)

    def login(self, label):
        return self.call('/api/v1/auth/login', 'POST', {
            'email': input(label + '邮箱: ').strip(), 'password': getpass.getpass(label + '密码: ')})['data']


def scan(client, path):
    cursor, seen, cursors, items = '', set(), set(), []
    for _ in range(300):
        page = client.call(path + '?' + urllib.parse.urlencode({'limit': 2, 'cursor': cursor}))['data']
        assert len(page['items']) <= 2
        for item in page['items']:
            assert item['comment_id'] not in seen, '评论分页重复'
            assert 'user_email' not in item and 'request_id' not in item, '响应泄露内部字段'
            if item['deleted_at']:
                assert item['content'] == '', '删除后仍返回正文'
            seen.add(item['comment_id'])
            items.append(item)
        if not page['has_more']:
            assert page['next_cursor'] == ''
            return items
        cursor = page['next_cursor']
        assert cursor and cursor not in cursors, '游标没有前进'
        cursors.add(cursor)
    raise AssertionError('超过600条评论，请使用专门测试视频')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    parser.add_argument('--video-id', required=True, type=int)
    parser.add_argument('--other-video-id', required=True, type=int)
    args = parser.parse_args()
    assert args.video_id > 0 and args.other_video_id > 0 and args.video_id != args.other_video_id, '提供两个不同的公开视频ID'
    a, b, guest = (Client(args.base_url) for _ in range(3))
    user_a, user_b = a.login('账号 A '), b.login('账号 B ')
    assert user_a['user_id'] != user_b['user_id'], '请使用两个不同账号'
    base = f'/api/v1/videos/{args.video_id}/comments'
    other = f'/api/v1/videos/{args.other_video_id}/comments'
    guest.call(f'/api/v1/videos/{args.video_id}')
    guest.call(f'/api/v1/videos/{args.other_video_id}')
    created = []

    def create(client, content, parent='0', token=None):
        command = {'content': content, 'parent_id': parent, 'request_id': token or str(uuid.uuid4())}
        item = client.call(base, 'POST', command)['data']
        if not any(comment_id == item['comment_id'] for _, comment_id in created):
            created.append((client, item['comment_id']))
        return item, command

    try:
        guest.call(base, 'POST', {'content': 'anonymous'}, expected=401)
        a.call(base, 'POST', {'content': ' ', 'request_id': str(uuid.uuid4())}, expected=400)
        a.call(base, 'POST', {'content': 'spoof', 'user_id': str(user_b['user_id']), 'request_id': str(uuid.uuid4())}, expected=400)
        root, command = create(a, 'L4-A 测试根评论')
        retry = a.call(base, 'POST', command)['data']
        assert retry['comment_id'] == root['comment_id'], '请求重试生成了重复评论'
        a.call(base, 'POST', {**command, 'content': '同一请求ID换内容'}, expected=409)
        for index in range(2):
            create(a, f'L4-A 分页测试 {index}')
        reply, _ = create(b, 'B 回复 A', root['comment_id'])
        nested, _ = create(a, 'A 回复 B', reply['comment_id'])
        assert reply['root_id'] == nested['root_id'] == root['comment_id']
        assert nested['parent_id'] == reply['comment_id'] and nested['reply_to_user_id'] == str(user_b['user_id'])
        b.call(other, 'POST', {'content': '串到另一个视频', 'parent_id': root['comment_id'], 'request_id': str(uuid.uuid4())}, expected=400)
        replies_path = base + '/' + root['comment_id'] + '/replies'
        guest.call(base + '/' + reply['comment_id'] + '/replies', expected=404)
        roots = scan(guest, base)
        assert all(item['root_id'] == '0' for item in roots)
        assert any(item['comment_id'] == root['comment_id'] and item['reply_count'] == 2 for item in roots)
        replies = scan(guest, replies_path)
        assert {item['comment_id'] for item in replies} == {reply['comment_id'], nested['comment_id']}
        first = guest.call(base + '?limit=1')['data']
        assert first['has_more']
        encoded = urllib.parse.urlencode({'cursor': first['next_cursor']})
        guest.call(other + '?' + encoded, expected=400)
        guest.call(replies_path + '?' + encoded, expected=400)
        b.call(base + '/' + root['comment_id'], 'DELETE', expected=404)
        a.call(base + '/' + root['comment_id'], 'DELETE')
        a.call(base + '/' + root['comment_id'], 'DELETE')
        tombstone = next(item for item in scan(guest, base) if item['comment_id'] == root['comment_id'])
        assert tombstone['deleted_at'] and tombstone['content'] == '' and tombstone['reply_count'] == 2
        assert len(scan(guest, replies_path)) == 2
        b.call(base, 'POST', {'content': '根已删', 'parent_id': reply['comment_id'], 'request_id': str(uuid.uuid4())}, expected=409)
        a.call(base + '/' + reply['comment_id'], 'DELETE', expected=404)
        b.call(base + '/' + reply['comment_id'], 'DELETE')
        assert next(item for item in scan(guest, replies_path) if item['comment_id'] == reply['comment_id'])['content'] == ''
        print('视频评论验收通过：发表/回复、幂等、独立分页、串帖拦截、权限及删除占位。')
    finally:
        for client, comment_id in reversed(created):
            try:
                client.call(base + '/' + comment_id, 'DELETE')
            except Exception as error:
                print(f'清理本次评论 {comment_id} 失败: {error}')
        print('本次创建评论的正文已尝试清除，删除占位保留；不会删除视频或原有评论。')


if __name__ == '__main__':
    main()
