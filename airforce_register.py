"""Api.Airforce 批量注册脚本 - 单窗口版"""

import random
import string
import json
import time
import subprocess
from DrissionPage import ChromiumPage, ChromiumOptions


def generate_username():
    return ''.join(random.choices(string.ascii_lowercase + string.digits, k=10))


def generate_password():
    pwd = [
        random.choice(string.ascii_lowercase),
        random.choice(string.ascii_uppercase),
        random.choice(string.digits),
    ]
    pwd += random.choices(string.ascii_letters + string.digits, k=9)
    random.shuffle(pwd)
    return ''.join(pwd)


def register_one(page):
    """注册一个账号"""
    username = generate_username()
    password = generate_password()
    
    # 访问注册页
    page.get("https://api.airforce/signup/")
    time.sleep(2)
    
    # 填写表单
    user_input = page.ele('css:input[placeholder*="用户名"]')
    if user_input:
        user_input.input(username)
    
    for inp in page.eles('css:input[type="password"]'):
        inp.input(password)
    
    time.sleep(2)  # 等待 Turnstile 自动验证
    
    # 点击注册
    btn = page.ele('text:创建免费账户')
    if btn:
        btn.click()
    
    # 等待跳转
    for i in range(10):
        time.sleep(1)
        if "dashboard" in page.url:
            break
    
    success = "dashboard" in page.url
    
    # 获取 API Key
    api_key = None
    if success:
        time.sleep(2)
        for _ in range(3):
            try:
                copy_btn = page.ele('text:复制', timeout=2)
                if copy_btn:
                    copy_btn.click()
                    time.sleep(1)
                    api_key = subprocess.run(['powershell', '-command', 'Get-Clipboard'], capture_output=True, text=True).stdout.strip()
                    if api_key and 'sk-air' in api_key:
                        break
            except:
                time.sleep(1)
    
    # 清除 cookies 和缓存
    try:
        page.clear_cache(cookies=True, session_storage=True, local_storage=True)
    except:
        pass
    
    return {
        "username": username,
        "password": password,
        "api_key": api_key,
        "success": success
    }


def main():
    count = 5
    
    print(f"\n开始批量注册 {count} 个账号...\n")
    
    # 只打开一次浏览器
    co = ChromiumOptions()
    co.set_argument('--incognito')
    page = ChromiumPage(co)
    page.set.window.max()
    
    accounts = []
    
    try:
        for i in range(count):
            print(f"[{i+1}/{count}] 注册中...")
            
            result = register_one(page)
            
            if result["success"]:
                accounts.append(result)
                print(f"  ✓ 注册成功 - {result['username']}")
                if result['api_key']:
                    print(f"    API: {result['api_key'][:20]}...")
                else:
                    print(f"    API: 获取失败")
            else:
                print(f"  ✗ 注册失败")
            
            # 保存进度
            with open("D:\\air\\accounts.json", "w", encoding="utf-8") as f:
                json.dump(accounts, f, indent=2, ensure_ascii=False)
            
            print()
            time.sleep(1)
        
        print("=" * 50)
        print(f"完成！成功 {len(accounts)}/{count} 个")
        print(f"已保存到 D:\\air\\accounts.json")
        
    finally:
        page.quit()


if __name__ == "__main__":
    main()
