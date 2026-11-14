#!/usr/bin/env python3
"""Real HTTP/MySQL/Kafka smoke test. Uses two existing test accounts, never grants roles."""
import argparse
import getpass
import http.cookiejar
import json
import struct
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zlib


class Client:
    def __init__(self, base):
        self.base = base.rstrip('/')
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, path, method='GET', body=None, expected=200, headers=None):
        if body is not None and not isinstance(body, bytes):
            body = json.dumps(body).encode()
            headers = {'Content-Type': 'application/json'}
        request = urllib.request.Request(self.base + path, data=body, method=method, headers=headers or {})
        try:
            response = self.opener.open(request, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            if response.status != expected:
                raise AssertionError(f'{method} {path}: expected {expected}, got {response.status}: {raw[:300]!r}')
            if 'application/json' in response.headers.get('Content-Type', ''):
                return json.loads(raw)
            return raw

    def login(self, label):
        email = input(f'{label}邮箱: ').strip()
        password = getpass.getpass(f'{label}密码: ')
        return self.call('/api/v1/auth/login', 'POST', {'email': email, 'password': password})['data']


def tiny_png():
    def chunk(kind, data):
        return struct.pack('!I', len(data)) + kind + data + struct.pack('!I', zlib.crc32(kind + data) & 0xffffffff)
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('!IIBBBBB', 1, 1, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(b'\x00\xff\x40\x30')) + chunk(b'IEND', b'')


def upload(client):
    boundary = uuid.uuid4().hex
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="test.png"\r\nContent-Type: image/png\r\n\r\n'.encode()
            + tiny_png() + f'\r\n--{boundary}--\r\n'.encode())
    return client.call('/api/v1/posts/images', 'POST', body, 201, {'Content-Type': 'multipart/form-data; boundary=' + boundary})['data']


def scan(client):
    cursor = ''
    cursors, seen, items = set(), set(), []
    for _ in range(100):
        result = client.call('/api/v1/feeds/following?' + urllib.parse.urlencode({'limit': 2, 'cursor': cursor}))['data']
        assert len(result['items']) <= 2
        for item in result['items']:
            assert item['event_id'] not in seen, '跨页动态重复'
            assert 'user_email' not in item['author'], '泄露作者邮箱'
            seen.add(item['event_id'])
            items.append(item)
        if not result['has_more']:
            assert result['next_cursor'] == ''
            return items
        cursor = result['next_cursor']
        assert cursor and cursor not in cursors, '游标不前进'
        cursors.add(cursor)
    raise AssertionError('超过200条，请使用关注较少的专用测试账号')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    parser.add_argument('--wait-seconds', type=int, default=60)
    args = parser.parse_args()
    author, viewer, guest = (Client(args.base_url) for _ in range(3))
    a, b = author.login('作者 A '), viewer.login('观众 B ')
    assert str(a['user_id']) != str(b['user_id']), '必须使用两个不同账号'
    relationship = '/api/v1/users/' + str(a['user_id']) + '/follow'
    was_following = viewer.call(relationship)['data']['following']
    created = []
    viewer_draft = None
    try:
        viewer.call(relationship, 'PUT')
        guest.call('/api/v1/feeds/following', expected=401)
        viewer.call('/api/v1/feeds/following?cursor=invalid', expected=400)
        viewer.call('/api/v1/feeds/following?limit=0', expected=400)
        image = upload(author)
        guest.call(image['url'], expected=404)
        author.call(image['url'])
        viewer_draft = viewer.call('/api/v1/posts', 'POST', expected=201)['data']
        viewer.call('/api/v1/posts/' + viewer_draft['post_id'], 'PATCH', {
            'version': viewer_draft['version'], 'content': '不能盗用图片', 'image_ids': [image['image_id']]}, expected=400)
        for index in range(3):
            draft = author.call('/api/v1/posts', 'POST', expected=201)['data']
            created.append(draft['post_id'])
            path = '/api/v1/posts/' + draft['post_id']
            guest.call(path, expected=404)
            viewer.call(path, 'DELETE', expected=404)
            draft = author.call(path, 'PATCH', {'version': draft['version'], 'content': f'L3-B integration test {index}',
                                                'image_ids': [image['image_id']] if index == 0 else []})['data']
            author.call(path, 'PATCH', {'version': '1', 'content': '旧版本修改', 'image_ids': []}, expected=409)
            author.call(path + '/publish', 'POST', {'version': draft['version']})
            author.call(path + '/publish', 'POST', {'version': draft['version']})
            guest.call(path)
        guest.call(image['url'])
        deadline = time.monotonic() + args.wait_seconds
        while True:
            items = scan(viewer)
            matching = [i for i in items if i.get('post', {}).get('post_id') in created]
            if len(matching) == 3:
                break
            if time.monotonic() >= deadline:
                raise AssertionError('动态未在等待时间内出现，请检查Outbox、Kafka和消费者日志')
            time.sleep(1)
        author.call('/api/v1/posts/' + created[0], 'DELETE')
        guest.call('/api/v1/posts/' + created[0], expected=404)
        guest.call(image['url'], expected=404)
        assert not any(i.get('post', {}).get('post_id') == created[0] for i in scan(viewer))
        viewer.call(relationship, 'DELETE')
        assert not any(i['author']['user_id'] == str(a['user_id']) for i in scan(viewer))
        print('L3-B 接口验收通过：发布/重复发布、动态分页、图片归属、草稿隔离、版本冲突、删除和取关过滤。')
    finally:
        # Only remove records created by this invocation; restore the viewer's prior relationship.
        for post_id in created:
            try:
                author.call('/api/v1/posts/' + post_id, 'DELETE')
            except Exception as error:
                if 'got 404:' not in str(error):
                    print(f'测试图文 {post_id} 清理失败: {error}')
        if viewer_draft:
            try:
                viewer.call('/api/v1/posts/' + viewer_draft['post_id'], 'DELETE')
            except Exception as error:
                print(f'观众草稿清理失败: {error}')
        viewer.call(relationship, 'PUT' if was_following else 'DELETE')


if __name__ == '__main__':
    main()
