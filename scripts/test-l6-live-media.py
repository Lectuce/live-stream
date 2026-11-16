#!/usr/bin/env python3
"""Real-media smoke test. Uses an owned test room; never prints the push key."""
import argparse
import http.cookiejar
import json
import shutil
import subprocess
import time
import urllib.parse
import urllib.request


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--base-url', default='http://127.0.0.1:9000')
    p.add_argument('--public-base', required=True)
    p.add_argument('--cookie', required=True)
    p.add_argument('--room', type=int, required=True)
    p.add_argument('--media', required=True)
    args = p.parse_args()
    if not shutil.which('ffmpeg'):
        raise RuntimeError('请先安装 ffmpeg')
    jar = http.cookiejar.MozillaCookieJar(args.cookie)
    jar.load(ignore_discard=True, ignore_expires=False)
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def api(path, body=None):
        req = urllib.request.Request(args.base_url.rstrip('/') + path,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers={'Content-Type': 'application/json'})
        with client.open(req, timeout=10) as response:
            return json.load(response)

    room_path = f'/api/v1/rooms/{args.room}'
    assert api(room_path)['data']['status'] == 0, '请使用未开播的测试房间'
    credentials = api(room_path + '/start', {'room_title': 'L6 媒体验收'})['data']
    encoder = None
    try:
        assert api(room_path)['data']['status'] == 0, '没有推流不应进入直播状态'
        repeated = api(room_path + '/start', {'room_title': 'L6 媒体验收'})['data']
        assert repeated['session_id'] == credentials['session_id'], '重复领取生成了新场次'
        encoder = subprocess.Popen(['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error',
                                    '-re', '-stream_loop', '-1', '-i', args.media,
                                    '-c', 'copy', '-f', 'flv',
                                    credentials['server'] + '/' + credentials['stream_key']],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            if encoder.poll() is not None:
                raise RuntimeError('FFmpeg 推流失败，请检查 H.264/AAC 输入、1935端口和 SRS 日志')
            detail = api(room_path)['data']
            if detail['status'] == 1:
                break
            time.sleep(1)
        else:
            raise AssertionError('45秒内未收到开播确认')
        assert detail['live_session_id'] == credentials['session_id']
        media_url = urllib.parse.urljoin(args.public_base.rstrip('/') + '/', detail['play_url'])
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(media_url, timeout=8) as response:
                    manifest = response.read(65536).decode()
                if '#EXTM3U' in manifest and '#EXTINF' in manifest:
                    break
            except OSError:
                pass
            time.sleep(2)
        else:
            raise AssertionError('HLS清单未生成或Nginx代理不可达')
        segment = next(line for line in manifest.splitlines() if line and not line.startswith('#'))
        with urllib.request.urlopen(urllib.parse.urljoin(media_url, segment), timeout=10) as response:
            assert response.read(188), 'HLS分片为空'
        print('通过：凭证幂等、真实推流、场次状态、HLS清单和分片')
    finally:
        # Revoke first; do not leave a live test room after an assertion fails.
        try:
            api(room_path + '/stop', {})
        finally:
            if encoder is not None:
                encoder.terminate()
                try:
                    encoder.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    encoder.kill()
                    encoder.wait()
    assert api(room_path)['data']['status'] == 0, '关播未生效'
    print('通过：关播与资源清理')


if __name__ == '__main__':
    main()
