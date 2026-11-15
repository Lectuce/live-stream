#!/usr/bin/env python3
"""L4-A real-server acceptance. Creates two disposable posts; requires two users and an admin."""
import argparse
import concurrent.futures
import runpy
import uuid
from pathlib import Path
from urllib.parse import urlencode

Client = runpy.run_path(str(Path(__file__).with_name('test-content-comments.py')))['Client']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://127.0.0.1:9000')
    args = parser.parse_args()
    a, b, admin, guest = [Client(args.base_url) for _ in range(4)]
    ua, ub, reviewer = a.login('作者 A '), b.login('用户 B '), admin.login('管理员 ')
    assert ua['user_id'] != ub['user_id'] and ua.get('role') != 'admin' and ub.get('role') != 'admin'
    assert reviewer.get('role') == 'admin', '第三个账号必须是管理员'
    posts, comments = [], []

    def create_post(publish=True):
        item = a.call('/api/v1/posts', 'POST', expected=201)['data']
        pid = item['post_id']; posts.append(pid)
        a.call(f'/api/v1/posts/{pid}', 'PATCH', {'version':item['version'], 'content':'L4-A 自动验收图文', 'image_ids':[]})
        item = a.call(f'/api/v1/posts/mine/{pid}')['data']
        if publish:
            a.call(f'/api/v1/posts/{pid}/publish', 'POST', {'version':item['version']})
        return pid

    def mine(cid):
        rows = a.call('/api/v1/comments/mine?' + urlencode({'target_type':'post','target_id':pid,'limit':50}))['data']['items']
        return next(item for item in rows if item['comment_id'] == cid)

    def review(cid, decision='approved', reason='自动验收'):
        version = mine(cid)['version']
        admin.call(f'/api/v1/admin/comments/{cid}/review', 'POST', {'version':version,'decision':decision,'reason':reason})

    def create(text, parent='0'):
        item = a.call(base, 'POST', {'content':text,'parent_id':parent,'request_id':str(uuid.uuid4())})['data']
        comments.append(item['comment_id'])
        return item['comment_id']

    def ids(path):
        return [item['comment_id'] for item in guest.call(path)['data']['items']]

    try:
        pid = create_post(); private = create_post(False)
        base = f'/api/v1/posts/{pid}/comments'
        guest.call(f'/api/v1/posts/{private}/comments', expected=404)
        a.call(f'/api/v1/posts/{private}/comments','POST',{'content':'private','request_id':str(uuid.uuid4())},expected=404)
        a.call('/api/v1/admin/comments', expected=403)
        b.call('/api/v1/admin/comment-reports', expected=403)
        root = create('根评论 A')
        assert root not in ids(base), '待审核评论被公开'
        assert mine(root)['audit_status'] == 'pending'
        a.call(f'/api/v1/admin/comments/{root}/review','POST',{'version':'1','decision':'approved'},expected=403)
        review(root, 'rejected', '请补充内容')
        assert mine(root)['audit_reason'] == '请补充内容'
        old = mine(root)['version']
        b.call(f'/api/v1/comments/{root}','PATCH',{'version':old,'content':'越权修改'},expected=404)
        a.call(f'/api/v1/comments/{root}','PATCH',{'version':old,'content':'补充后的正文'})
        admin.call(f'/api/v1/admin/comments/{root}/review','POST',{'version':old,'decision':'approved'},expected=409)
        review(root)
        assert root in ids(base)
        child = create('对子评论的回复', root); review(child)
        nested = create('第三次回复仍归同一根', child); review(nested)
        assert mine(nested)['root_id'] == root
        reply_path = base + '/' + root + '/replies'
        assert set(ids(reply_path)) == {child, nested}

        # Concurrent repeats must leave one relationship and one count.
        def like(method):
            return b.call(f'/api/v1/comments/{root}/like',method)['data']
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
            results = list(pool.map(lambda _: like('PUT'), range(12)))
        assert all(row['like_count'] == 1 and row['liked'] for row in results), results
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
            results = list(pool.map(lambda _: like('DELETE'), range(12)))
        assert all(row['like_count'] == 0 and not row['liked'] for row in results), results

        middle = create('热门候选 B'); review(middle)
        newest = create('热门候选 C'); review(newest)
        first = guest.call(base+'?sort=hot&limit=1')['data']
        assert first['items'][0]['comment_id'] == newest and first['has_more']
        encoded = urlencode({'sort':'hot','limit':1,'cursor':first['next_cursor']})
        guest.call(base+'?'+urlencode({'sort':'latest','cursor':first['next_cursor']}),expected=400)
        review(middle,'hidden','验收隐藏')
        second = guest.call(base+'?'+encoded)['data']
        assert second['items'][0]['comment_id'] == root, '旧快照未过滤隐藏候选或扫描游标跳过了有效项'
        assert middle not in ids(base)

        review(root,'hidden','验收整条讨论隐藏')
        guest.call(reply_path, expected=404)
        assert root not in ids(base)
        b.call(f'/api/v1/comments/{child}/like','PUT',expected=409)
        review(root)
        assert set(ids(reply_path)) == {child,nested}

        for _ in range(2):
            b.call(f'/api/v1/comments/{root}/reports','POST',{'reason':'测试举报'})
        reports = admin.call('/api/v1/admin/comment-reports?limit=50')['data']['items']
        matching = [item for item in reports if item['comment_id']==root and str(item['user_id'])==str(ub['user_id'])]
        assert len(matching)==1, '重复举报生成了多条记录'
        report_id = matching[0]['report_id']
        b.call(f'/api/v1/admin/comment-reports/{report_id}/resolve','POST',{'status':'dismissed','reason':'越权'},expected=403)
        for _ in range(2):
            admin.call(f'/api/v1/admin/comment-reports/{report_id}/resolve','POST',{'status':'dismissed','reason':'验收完成，无违规'})
        history = admin.call(f'/api/v1/admin/comments/{root}/history?limit=50')['data']['items']
        assert {'edited','rejected','approved','hidden'} <= {item['action'] for item in history}
        assert all('content' not in item for item in history), '历史接口不应保存已删除原文'

        a.call(base+'/'+root,'DELETE')
        row = next(item for item in guest.call(base)['data']['items'] if item['comment_id']==root)
        assert row['content']=='' and row['deleted_at']
        assert set(ids(reply_path))=={child,nested}
        print('L4-A验收通过：待审可见性、编辑重审、版本冲突、两级回复、并发点赞幂等、热门快照过滤、隐藏传播、举报处理和审计。')
    finally:
        for cid in reversed(comments):
            try: a.call(f'/api/v1/posts/{pid}/comments/{cid}','DELETE')
            except Exception as error: print(f'清理测试评论 {cid} 失败: {error}')
        for item in posts:
            try: a.call(f'/api/v1/posts/{item}','DELETE')
            except Exception as error: print(f'清理测试图文 {item} 失败: {error}')
        print('仅清理本次创建的图文和评论正文；删除占位、审计及举报记录保留。')


if __name__ == '__main__':
    main()
