import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fake_server import FakeServer

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
CLI_EXE = os.path.join(REPO, "cli.exe")

NODE_NAMES = ["fake-trojan", "fake-ss", "fake-vmess", "fake-vless", "fake-socks5", "fake-http"]
NODE_OPTIONS = {
    "fake-trojan": {"password": "fake-trojan-pass", "servername": "fake.test", "skip-cert-verify": "true"},
    "fake-ss": {"cipher": "chacha20-ietf-poly1305", "password": "fake-ss-pass"},
    "fake-vmess": {"cipher": "auto", "uuid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
    "fake-vless": {"network": "tcp", "uuid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
    "fake-socks5": {},
    "fake-http": {},
}


def wait_port(port, timeout=10):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=1):
                return True
        except OSError:
            time.sleep(0.1)
    return False


def run_cli(workdir, args, expect_ok=True):
    proc = subprocess.run(
        [CLI_EXE] + args,
        cwd=workdir,
        capture_output=True,
        timeout=120,
    )
    out = proc.stdout.decode("utf-8", errors="replace")
    if expect_ok and proc.returncode != 0:
        raise AssertionError(f"cli {' '.join(args)} exit {proc.returncode}:\n{out}\n{proc.stderr.decode(errors='replace')}")
    return out


def main():
    tmpdir = tempfile.mkdtemp(prefix="fakeserver-e2e-")
    srv = FakeServer(tmpdir)
    srv.start()
    time.sleep(0.3)
    for name, port in srv.ports.items():
        if name == "ss_udp":
            continue
        if not wait_port(port):
            raise AssertionError(f"port {name} ({port}) not listening")

    workdir = tempfile.mkdtemp(prefix="fakecli-")
    shutil.copy(CLI_EXE, workdir)
    sub_url = f"http://127.0.0.1:{srv.ports['sub']}/sub.yaml"
    results = []

    def check(step, cond, detail=""):
        results.append((step, cond, detail))
        mark = "PASS" if cond else "FAIL"
        print(f"[{mark}] {step}" + (f" -- {detail}" if detail and not cond else ""))

    out = run_cli(workdir, ["sub", "add", sub_url, "FakeProvider"])
    check("sub add", "导入成功: FakeProvider" in out and "节点数: 6" in out, out)
    check("sub add 代理组", "代理组: 1" in out, out)

    out = run_cli(workdir, ["sub", "list"])
    check("sub list 余量", "94.00 GB / 100.00 GB" in out, out)
    check("sub list 到期", "到期 2030-01-01" in out, out)

    out = run_cli(workdir, ["sub", "use", "FakeProvider"])
    check("sub use", "已启用: FakeProvider" in out and "可用代理组: FakeGroup" in out, out)

    out = run_cli(workdir, ["sub", "nodes", "FakeProvider"])
    check("sub nodes 全部节点", all(n in out for n in NODE_NAMES), out)
    check("sub nodes UDP 标记", out.count("[TCP+UDP]") == 5, out)

    out = run_cli(workdir, ["sub", "select", "FakeGroup", "fake-trojan"])
    check("sub select", "组 FakeGroup 已切换到 fake-trojan" in out, out)

    out = run_cli(workdir, ["sub", "current"])
    check("sub current 选择持久化", "组 FakeGroup -> fake-trojan" in out, out)

    out = run_cli(workdir, ["sub", "test", "FakeProvider"])
    check("sub test 全可用", "完成: 6 可用, 0 失败" in out, out)

    out = run_cli(workdir, ["sub", "update", "FakeProvider"])
    check("sub update", "更新成功: FakeProvider  6 节点" in out, out)

    out = run_cli(workdir, ["sub", "current"])
    check("sub update 后选择保留", "组 FakeGroup -> fake-trojan" in out, out)

    store_path = os.path.join(workdir, "rules", "subscriptions.json")
    with open(store_path, "r", encoding="utf-8") as f:
        store = json.load(f)
    entry = next(e for e in store["entries"] if e["name"] == "FakeProvider")
    check("store 落盘余量", entry["user_info"]["total"] == 107374182400, json.dumps(entry["user_info"]))
    check("store 落盘到期", entry["user_info"]["expire_time"] == 1893456000, str(entry["user_info"]))
    check("store 规则条数", len(entry["site_groups"]) >= 1 and any(
        "~.+" in sg["domains"] for sg in entry["site_groups"]
    ), json.dumps(entry["site_groups"], ensure_ascii=False))

    nodes_json = os.path.join(tmpdir, "nodes.json")
    specs = [
        {"name": n, "type": n.replace("fake-", ""), "server": "127.0.0.1",
         "port": srv.ports[n.replace("fake-", "")], "options": NODE_OPTIONS[n]}
        for n in NODE_NAMES
    ]
    with open(nodes_json, "w") as f:
        json.dump(specs, f)
    rt_exe = os.path.join(tmpdir, "roundtrip.exe")
    build = subprocess.run(
        ["go", "build", "-o", rt_exe, "./test/fakeserver/roundtrip"],
        cwd=REPO, capture_output=True, timeout=300,
    )
    if build.returncode != 0:
        raise AssertionError("roundtrip build failed: " + build.stderr.decode(errors="replace"))
    proc = subprocess.run([rt_exe, nodes_json], capture_output=True, timeout=120)
    out = proc.stdout.decode("utf-8", errors="replace")
    print(out.strip())
    check("协议回环 roundtrip", proc.returncode == 0 and out.count("OK ") == 6,
          out + proc.stderr.decode(errors="replace"))

    srv.stop()

    failed = [r for r in results if not r[1]]
    print(f"\n{'=' * 40}")
    print(f"总计: {len(results) - len(failed)}/{len(results)} 通过")
    if failed:
        for step, _, detail in failed:
            print(f"  FAILED: {step}\n{detail}")
        sys.exit(1)
    print("全部通过")


if __name__ == "__main__":
    main()
