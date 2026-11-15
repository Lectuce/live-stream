#!/usr/bin/env python3
"""L4-B acceptance on a real server. Use dedicated author/viewer/admin accounts."""
import argparse
import concurrent.futures
import runpy
import time
import uuid
from pathlib import Path
from urllib.parse import urlencode

Client = runpy.run_path(str(Path(__file__).with_name('test-content-comments.py')))['Client']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    parser.add_argument('--video-id', required=True, help='A public video; its content and publication state are not changed')
    args = parser.parse_args()
    a, b, admin, guest = [Client(args.base_url) for _ in range(4)]
    ua, ub, reviewer = a.login('作者 A '), b.login('观众 B '), admin.login('管理员 ')
    assert len({ua['user_id'], ub['user_id'], reviewer['user_id']}) == 3
    assert ua.get('role') != 'admin' and ub.get('role') != 'admin' and reviewer.get('role') == 'admin'
    pid = folder = None
    following_path = f"/api/v1/users/{ua['user_id']}/follow"
    was_following = b.call(following_path)['data']['following']

    def scan(client, path, key):
        rows, cursor, seen = [], '', set()
        for _ in range(100):
            sep = '&' if '?' in path else '?'
            page = client.call(path + sep + urlencode({'cursor':cursor, 'limit':2}))['data']
            for item in page['items']:
                assert item[key] not in seen, '分页重复'
                seen.add(item[key]); rows.append(item)
            if not page['has_more']:
                assert page['next_cursor'] == ''
                return rows
            assert page['next_cursor'] and page['next_cursor'] != cursor
            cursor = page['next_cursor']
        raise AssertionError('测试账号数据超过200条，请换专用测试账号')

    def wait_notice(client, predicate):
        for _ in range(15):
            matches = [n for n in scan(client, '/api/v1/me/notifications', 'notification_id') if predicate(n)]
            if matches:
                assert len(matches) == 1, '同一事件出现重复通知'
                return matches[0]
            time.sleep(1)
        raise AssertionError('15秒内未收到通知，请检查 notification outbox 日志')

    def comment(client, text, parent='0'):
        data = client.call(f'/api/v1/posts/{pid}/comments', 'POST', {'content':text, 'parent_id':parent, 'request_id':str(uuid.uuid4())})['data']
        return data['comment_id']

    def approve(client, cid):
        path = '/api/v1/comments/mine?' + urlencode({'target_type':'post','target_id':pid})
        item = next(c for c in scan(client,path,'comment_id') if c['comment_id'] == cid)
        admin.call(f'/api/v1/admin/comments/{cid}/review','POST',{'version':item['version'],'decision':'approved','reason':'L4-B验收'})

    try:
        guest.call('/api/v1/me/notifications',expected=401)
        a.call('/api/v1/admin/reports',expected=403)
        guest.call(f'/api/v1/videos/{args.video_id}')
        p=a.call('/api/v1/posts','POST',expected=201)['data']; pid=p['post_id']
        p=a.call(f'/api/v1/posts/{pid}','PATCH',{'version':p['version'],'content':'L4-B 自动验收临时图文','image_ids':[]})['data']
        a.call(f'/api/v1/posts/{pid}/publish','POST',{'version':p['version']})
        like=f'/api/v1/posts/{pid}/like'
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            results=list(pool.map(lambda _:b.call(like,'PUT')['data'],range(6)))
        assert all(s['liked'] and s['like_count']==1 for s in results)
        assert not a.call(like)['data']['liked'], '点赞身份串号'
        for _ in range(2): assert b.call(like,'DELETE')['data']=={'liked':False,'like_count':0}
        print('PASS 一人一赞、并发重试、取消幂等')

        folders=b.call('/api/v1/me/favorite-folders')['data']
        default=next(f for f in folders if f['is_default'])
        b.call(f"/api/v1/me/favorite-folders/{default['folder_id']}",'DELETE',expected=409)
        folder=b.call('/api/v1/me/favorite-folders','POST',{'name':'L4-B 临时收藏夹'},expected=201)['data']['folder_id']
        fp=f'/api/v1/me/favorite-folders/{folder}'
        a.call(fp+'/items',expected=404)
        a.call(fp,'PATCH',{'name':'越权'},expected=404)
        a.call(fp,'DELETE',expected=404)
        item_path=fp+'/items/'+args.video_id
        a.call(item_path,'PUT',expected=404)
        for _ in range(2): b.call(item_path,'PUT')
        items=scan(b,fp+'/items','item_id')
        assert len(items)==1 and items[0]['available'] and 'play_url' not in items[0]
        assert folder in b.call('/api/v1/me/favorites/'+args.video_id)['data']
        b.call(fp,'PATCH',{'name':'L4-B 已改名'})
        for _ in range(2): b.call(item_path,'DELETE')
        assert not scan(b,fp+'/items','item_id')
        print('PASS 收藏夹私有、重复收藏、移除、重命名')

        b.call(following_path,'DELETE')
        previous={n['notification_id'] for n in scan(a,'/api/v1/me/notifications','notification_id')}
        for _ in range(2): b.call(following_path,'PUT')
        n=wait_notice(a,lambda n:n['kind']=='follow' and n['actor_id']==str(ub['user_id']) and n['notification_id'] not in previous)
        nid=n['notification_id']
        b.call(f'/api/v1/me/notifications/{nid}/read','PUT',expected=404)
        a.call(f'/api/v1/me/notifications/{nid}/read','PUT')
        a.call(f'/api/v1/me/notifications/{nid}/read','PUT')
        root=comment(a,'根评论');approve(a,root)
        reply=comment(b,'对作者的回复',root);approve(b,reply)
        n=wait_notice(a,lambda n:n['kind']=='comment.reply' and n['target_id']==reply)
        assert n['url']==f'/posts/{pid}' and 'content' not in n
        wait_notice(b,lambda n:n['kind']=='comment.review' and n['target_id']==reply)
        a.call('/api/v1/me/notifications/read','POST',{'before':n['notification_id']})
        print('PASS 关注及回复通知、去重、接收者已读权限')

        for _ in range(2): b.call('/api/v1/reports','POST',{'target_type':'post','target_id':pid,'reason':'L4-B 测试举报'})
        reports=scan(admin,'/api/v1/admin/reports?status=pending&target_type=post','report_id')
        matches=[r for r in reports if r['target_id']==pid and r['user_id']==str(ub['user_id'])]
        assert len(matches)==1
        r=matches[0]; rp=f"/api/v1/admin/reports/{r['report_id']}"
        b.call(rp+'/resolve','POST',{'action':'hidden','version':r['version'],'reason':'越权'},expected=403)
        admin.call(rp+'/resolve','POST',{'action':'hidden','version':'0','reason':'无版本'},expected=400)
        admin.call(rp+'/resolve','POST',{'action':'hidden','version':r['version'],'reason':'验收隐藏'})
        guest.call(f'/api/v1/posts/{pid}',expected=404)
        guest.call(like,expected=404)
        own=a.call(f'/api/v1/posts/mine/{pid}')['data']
        a.call(f'/api/v1/posts/{pid}/publish','POST',{'version':own['version']},expected=409)
        notices=scan(a,'/api/v1/me/notifications','notification_id')
        assert all(n['url']=='' and 'content' not in n for n in notices if n['kind']=='comment.reply' and n['target_id']==reply)
        admin.call(rp+'/resolve','POST',{'action':'restored','version':r['version'],'reason':'旧版本'},expected=409)
        admin.call(rp+'/resolve','POST',{'action':'restored','version':own['version'],'reason':'验收恢复'})
        own=a.call(f'/api/v1/posts/mine/{pid}')['data'];assert own['status']=='draft'
        history=scan(admin,rp+'/history','action_id');assert {h['action'] for h in history}=={'hidden','restored'}
        print('PASS 举报去重、越权拒绝、版本保护、隐藏后不可见、管理员日志')
        print('L4-B 接口验收通过；仍需按文档检查浏览器操作、失效视频收藏及限流阈值。')
    finally:
        for path in ([f'/api/v1/me/favorite-folders/{folder}'] if folder else []):
            try:b.call(path,'DELETE')
            except Exception as exc:print('收藏夹清理失败:',exc)
        if pid:
            try:a.call(f'/api/v1/posts/{pid}','DELETE')
            except Exception as exc:print('临时图文清理失败:',exc)
        try:b.call(following_path,'PUT' if was_following else 'DELETE')
        except Exception as exc:print('恢复关注关系失败:',exc)


if __name__=='__main__':
    main()
