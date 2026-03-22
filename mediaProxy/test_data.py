import requests
import hashlib
import time

url = "http://127.0.0.1:5575/proxy?thread=6&chunkSize=256&url=https%3A%2F%2Fvod3379-aliyun04-vip-lixian.xunlei.com%2Fdownload%2F%3Ffid%3DADEJ-U770pd-KUWVHluMvw*odhrgCuEOAAAAAM21Q7mBWcQ*gECXy9cFLFm3n9P1%26mid%3D666%26threshold%3D251%26tid%3D57DC1FDAB5CD06F7E7DD31B5B0F082B7%26srcid%3D0%26verno%3D2%26pk%3Dxdrive%26e%3D1774147633%26g%3DCDB543B98159C43E804097CBD7052C59B79FD3F5%26i%3D003109FD4EFBD2977F2945951E5B8CBF0FA8761A%26ui%3D888880000085116%26t%3D1%26hy%3D0%26ms%3D8243280%26th%3D824328%26pt%3D0%26f%3D249629408%26alt%3D0%26pks%3D654%26rts%3D%26us%3D0%26hspu%3D%26po%3D0%26css%3D661498228%26rsrc%3D102%26ctrsc%3D1%26mtp%3Dvideo_mp2t%26vb%3D2198215%26pr%3D300%26rg%3D0-82432800%26spr%3Dplaytrans%26clientid%3DXqp0kJBXWhwaTpB6%26projectid%3D2rvk4e3gkdnl7u1kl0k%26share_user_id%3D%26vc%3Dhevc%26source%3Dxdrive%26cliplugver%3D%26tras%3D1%26fileid%3DVOn_0MOi1VqQ2NkijYYEbyOgA1%26fext%3Dmp4%26userid%3D%26vip%3DFREE%26clientver%3D%26at%3DF935152B36F10D0252C04E3862B14CB2"

def get_md5(url_to_test):
    print(f"Downloading from {url_to_test}...")
    m = hashlib.md5()
    resp = requests.get(url_to_test, stream=True, headers={"Range": "bytes=0-5000000"}, verify=False)
    print("Status:", resp.status_code)
    print("Content-Length:", resp.headers.get("Content-Length"))
    downloaded = 0
    start = time.time()
    for chunk in resp.iter_content(chunk_size=1024*1024):
        if chunk:
            m.update(chunk)
            downloaded += len(chunk)
    print(f"Downloaded {downloaded} bytes in {time.time()-start:.2f}s")
    return m.hexdigest()

direct_url = "https://vod3379-aliyun04-vip-lixian.xunlei.com/download/?fid=ADEJ-U770pd-KUWVHluMvw*odhrgCuEOAAAAAM21Q7mBWcQ*gECXy9cFLFm3n9P1&mid=666&threshold=251&tid=57DC1FDAB5CD06F7E7DD31B5B0F082B7&srcid=0&verno=2&pk=xdrive&e=1774147633&g=CDB543B98159C43E804097CBD7052C59B79FD3F5&i=003109FD4EFBD2977F2945951E5B8CBF0FA8761A&ui=888880000085116&t=1&hy=0&ms=8243280&th=824328&pt=0&f=249629408&alt=0&pks=654&rts=&us=0&hspu=&po=0&css=661498228&rsrc=102&ctrsc=1&mtp=video_mp2t&vb=2198215&pr=300&rg=0-82432800&spr=playtrans&clientid=Xqp0kJBXWhwaTpB6&projectid=2rvk4e3gkdnl7u1kl0k&share_user_id=&vc=hevc&source=xdrive&cliplugver=&tras=1&fileid=VOn_0MOi1VqQ2NkijYYEbyOgA1&fext=mp4&userid=&vip=FREE&clientver=&at=F935152B36F10D0252C04E3862B14CB2"

md5_direct = get_md5(direct_url)
print("Direct MD5:", md5_direct)

md5_proxy = get_md5(url)
print("Proxy MD5:", md5_proxy)

if md5_direct == md5_proxy:
    print("SUCCESS: Data matches exactly!")
else:
    print("ERROR: Data mismatch!")
