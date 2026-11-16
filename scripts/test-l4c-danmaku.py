#!/usr/bin/env python3
"""Video danmaku acceptance against a public test video; uses author, viewer and admin accounts."""
import argparse
import runpy
import uuid
from pathlib import Path
from urllib.parse import urlencode

Client=runpy.run_path(str(Path(__file__).with_name('test-content-comments.py')))['Client']

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url',default='http://127.0.0.1:9000')
    parser.add_argument('--video-id',required=True)
    args=parser.parse_args()
    a,b,admin,guest=[Client(args.base_url) for _ in range(4)]
    ua,ub,reviewer=a.login('发送者 A '),b.login('观众 B '),admin.login('管理员 ')
    assert len({ua['user_id'],ub['user_id'],reviewer['user_id']})==3 and reviewer['role']=='admin'
    video=guest.call('/api/v1/videos/'+args.video_id)['data']
    assert video['duration_ms']>10000,'测试视频需要长于10秒'
    path=f'/api/v1/videos/{args.video_id}/danmakus'
    created=[]
    def scan():
        cursor='';seen=set();rows=[]
        for _ in range(100):
            page=guest.call(path+'?'+urlencode({'from_ms':0,'to_ms':30000,'limit':2,'cursor':cursor}))['data']
            for item in page['items']:
                assert item['danmaku_id'] not in seen
                seen.add(item['danmaku_id']);rows.append(item)
            if not page['has_more']:return rows
            assert page['next_cursor'] and page['next_cursor']!=cursor
            cursor=page['next_cursor']
        raise AssertionError('请使用弹幕较少的测试视频')
    try:
        payload={'position_ms':10000,'content':'L4-C 时间轴验收','color':'#ffffff','request_id':str(uuid.uuid4())}
        guest.call(path,'POST',payload,expected=401)
        first=a.call(path,'POST',payload)['data'];created.append(first['danmaku_id'])
        assert a.call(path,'POST',payload)['data']['danmaku_id']==first['danmaku_id']
        a.call(path,'POST',dict(payload,content='重复ID但不同内容'),expected=409)
        for _ in range(3):
            item=a.call(path,'POST',dict(payload,request_id=str(uuid.uuid4())))['data'];created.append(item['danmaku_id'])
        a.call(path,'POST',dict(payload,request_id=str(uuid.uuid4()),position_ms=video['duration_ms']),expected=400)
        a.call(path,'POST',dict(payload,request_id=str(uuid.uuid4()),color='red'),expected=400)
        page=scan();assert set(created)<={i['danmaku_id'] for i in page}
        assert [(i['position_ms'],int(i['danmaku_id'])) for i in page]==sorted((i['position_ms'],int(i['danmaku_id'])) for i in page)
        b.call(path+'/'+created[0],'DELETE',expected=404)
        cursor=guest.call(path+'?from_ms=0&to_ms=30000&limit=1')['data']['next_cursor']
        guest.call(path+'?'+urlencode({'from_ms':30000,'to_ms':60000,'cursor':cursor}),expected=400)
        b.call('/api/v1/reports','POST',{'target_type':'video_danmaku','target_id':created[0],'reason':'L4-C 测试隐藏'})
        reports=admin.call('/api/v1/admin/reports?target_type=video_danmaku&status=pending&limit=50')['data']['items']
        r=next(r for r in reports if r['target_id']==created[0])
        rp=f"/api/v1/admin/reports/{r['report_id']}/resolve"
        admin.call(rp,'POST',{'action':'hidden','version':r['version'],'reason':'验收隐藏'})
        assert created[0] not in {i['danmaku_id'] for i in scan()}
        admin.call(rp,'POST',{'action':'restored','version':r['version'],'reason':'旧版本'},expected=409)
        admin.call(rp,'POST',{'action':'restored','version':str(int(r['version'])+1),'reason':'验收恢复'})
        assert created[0] in {i['danmaku_id'] for i in scan()}
        a.call(path+'/'+created[0],'DELETE');a.call(path+'/'+created[0],'DELETE')
        assert created[0] not in {i['danmaku_id'] for i in scan()}
        print('PASS: 同毫秒分页、幂等、时长/样式、窗口绑定、归属、举报隐藏/恢复和删除')
    finally:
        for item in created:
            try:a.call(path+'/'+item,'DELETE')
            except Exception as exc:print('清理失败:',item,exc)

if __name__=='__main__':main()
