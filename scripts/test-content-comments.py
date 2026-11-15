#!/usr/bin/env python3
"""Video/post comment smoke test against published content and two test accounts."""
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
    raise AssertionError('超过600条评论，请使用专门测试内容')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    parser.add_argument('--video-id', type=int)
    parser.add_argument('--other-video-id', type=int)
    parser.add_argument('--post-id', type=int)
    parser.add_argument('--other-post-id', type=int)
    parser.add_argument('--private-post-id', type=int, help='Optional draft or deleted post; only checks rejection, never changes it')
    args = parser.parse_args()
    kind = 'post' if args.post_id is not None else 'video'
    target_id = args.post_id if kind == 'post' else args.video_id
    other_id = args.other_post_id if kind == 'post' else args.other_video_id
    if not target_id or not other_id or target_id < 1 or other_id < 1 or target_id == other_id:
        parser.error('提供两个不同的公开图文ID（--post-id / --other-post-id），或两个公开视频ID（--video-id / --other-video-id）')
    cross = f'/api/v1/videos/{args.video_id}/comments' if kind == 'post' and args.video_id else None
    if kind == 'post' and not cross:
        parser.error('图文模式还需 --video-id，用于验证视频/图文隔离；推荐选择与图文相同数字的公开视频ID')
    a, b, guest = (Client(args.base_url) for _ in range(3))
    user_a, user_b = a.login('账号 A '), b.login('账号 B ')
    assert user_a['user_id'] != user_b['user_id'], '请使用两个不同账号'
    base = f'/api/v1/{kind}s/{target_id}/comments'
    other = f'/api/v1/{kind}s/{other_id}/comments'
    guest.call(base.removesuffix('/comments'))
    guest.call(other.removesuffix('/comments'))
    if cross:
        guest.call(cross.removesuffix('/comments'))
    created = []

    def create(client, content, parent='0', token=None):
        command = {'content': content, 'parent_id': parent, 'request_id': token or str(uuid.uuid4())}
        item = client.call(base, 'POST', command)['data']
        if not any(comment_id == item['comment_id'] for _, comment_id in created):
            created.append((client, item['comment_id']))
        return item, command

    try:
        if args.private_post_id:
            private = f'/api/v1/posts/{args.private_post_id}/comments'
            guest.call(private, expected=404)
            a.call(private, 'POST', {'content': '不可见图文', 'request_id': str(uuid.uuid4())}, expected=404)
        guest.call(base, 'POST', {'content': 'anonymous'}, expected=401)
        a.call(base, 'POST', {'content': ' ', 'request_id': str(uuid.uuid4())}, expected=400)
        a.call(base, 'POST', {'content': 'spoof', 'user_id': str(user_b['user_id']), 'request_id': str(uuid.uuid4())}, expected=400)
        root, command = create(a, 'L4-A 测试根评论')
        assert root['target_type'] == kind and root['target_id'] == str(target_id)
        if cross:
            a.call(cross, 'POST', command, expected=409)
            b.call(cross, 'POST', {'content': '跨类型回复', 'parent_id': root['comment_id'], 'request_id': str(uuid.uuid4())}, expected=400)
            guest.call(cross + '/' + root['comment_id'] + '/replies', expected=404)
            a.call(cross + '/' + root['comment_id'], 'DELETE', expected=404)
        retry = a.call(base, 'POST', command)['data']
        assert retry['comment_id'] == root['comment_id'], '请求重试生成了重复评论'
        a.call(base, 'POST', {**command, 'content': '同一请求ID换内容'}, expected=409)
        for index in range(2):
            create(a, f'L4-A 分页测试 {index}')
        reply, _ = create(b, 'B 回复 A', root['comment_id'])
        nested, _ = create(a, 'A 回复 B', reply['comment_id'])
        assert reply['root_id'] == nested['root_id'] == root['comment_id']
        assert nested['parent_id'] == reply['comment_id'] and nested['reply_to_user_id'] == str(user_b['user_id'])
        b.call(other, 'POST', {'content': '串到另一条内容', 'parent_id': root['comment_id'], 'request_id': str(uuid.uuid4())}, expected=400)
        replies_path = base + '/' + root['comment_id'] + '/replies'
        guest.call(base + '/' + reply['comment_id'] + '/replies', expected=404)
        roots = scan(guest, base)
        assert all(item['root_id'] == '0' and item['target_type'] == kind and item['target_id'] == str(target_id) for item in roots)
        assert any(item['comment_id'] == root['comment_id'] and item['reply_count'] == 2 for item in roots)
        replies = scan(guest, replies_path)
        assert {item['comment_id'] for item in replies} == {reply['comment_id'], nested['comment_id']}
        first = guest.call(base + '?limit=1')['data']
        assert first['has_more']
        encoded = urllib.parse.urlencode({'cursor': first['next_cursor']})
        guest.call(other + '?' + encoded, expected=400)
        if cross:
            guest.call(cross + '?' + encoded, expected=400)
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
        print(f'{kind} 评论验收通过：发表/回复、幂等、独立分页、串帖拦截、权限及删除占位。')
    finally:
        for client, comment_id in reversed(created):
            try:
                client.call(base + '/' + comment_id, 'DELETE')
            except Exception as error:
                print(f'清理本次评论 {comment_id} 失败: {error}')
        print('本次创建评论的正文已尝试清除，删除占位保留；不会删除视频、图文或原有评论。')


if __name__ == '__main__':
    main()
