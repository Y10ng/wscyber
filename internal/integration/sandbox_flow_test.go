package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
)

// 双重显式启用：专用测试库 URL + WSCYBER_INTEGRATION_DOCKER=1。
// 要求本机预先存在 wscyber/runtime:local、runc 与 Python3；不下载镜像、不启动 Docker。
// 只经真实 Executor 创建/核验/清理本次容器，测试没有 Docker CLI 或全局清理操作。
func TestSandboxIsolationAndPersistedEvidence(t *testing.T) {
	if os.Getenv("WSCYBER_INTEGRATION_DOCKER") != "1" {
		t.Skip("未设置 WSCYBER_INTEGRATION_DOCKER=1，跳过真实沙箱验收")
	}
	requireIntegrationDatabase(t)
	var hostHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostHits.Add(1)
		_, _ = w.Write([]byte("host-only-local-fixture-marker"))
	}))
	t.Cleanup(target.Close)
	h := newHarness(t, nil, execution.Config{
		Image: "wscyber/runtime:local", Runtime: "runc", Network: "none", Timeout: 15 * time.Second,
	})
	_, thread := h.createThread(t, target.URL)
	encodedTarget, err := json.Marshal(target.URL + "/sandbox-host-fixture")
	if err != nil {
		t.Fatal(err)
	}
	// statvfs 的只读挂载标志和实际写入失败同时检查，不能把普通权限拒绝冒充只读根文件系统。
	// 网络探测只指向本例宿主 httptest 的 loopback 端口，不使用外部地址、代理或 DNS。
	command := `python3 -I - <<'WSCYBER_INTEGRATION_PY'
import http.client
import json
import os
import socket
import urllib.parse

report = {
    "uid": os.getuid(),
    "euid": os.geteuid(),
    "rootfs_readonly": bool(os.statvfs("/").f_flag & os.ST_RDONLY),
    "root_write_succeeded": False,
    "root_write_errno": 0,
    "interfaces": sorted(name for _, name in socket.if_nameindex()),
    "host_tcp_reachable": False,
    "host_http_reachable": False,
    "host_error": "",
}
try:
    fd = os.open("/wscyber-integration-write-probe", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.close(fd)
    report["root_write_succeeded"] = True
except OSError as exc:
    report["root_write_errno"] = exc.errno

target = urllib.parse.urlsplit(` + string(encodedTarget) + `)
connection = http.client.HTTPConnection(target.hostname, target.port, timeout=2)
try:
    connection.connect()
    report["host_tcp_reachable"] = True
    connection.request("GET", target.path)
    response = connection.getresponse()
    report["host_http_reachable"] = True
    report["host_status"] = response.status
    report["host_body"] = response.read(128).decode("utf-8", errors="replace")
except (OSError, http.client.HTTPException) as exc:
    report["host_error"] = type(exc).__name__
finally:
    connection.close()
print(json.dumps(report, sort_keys=True))
WSCYBER_INTEGRATION_PY`
	var action domain.Action
	h.call(t, "POST", "/api/threads/"+thread.ID+"/actions", domain.ActionInput{
		Kind: "local_command", Command: command, Reason: "仅本地沙箱验收：非 root、只读根与无目标直连，并保存真实输出",
	}, http.StatusCreated, &action)
	if action.Status != "approved" {
		t.Fatal("工具命令应默认放行，不逐条审批", action)
	}
	snapshot := h.waitSnapshot(t, thread.ID, "真实沙箱退出、清理及结果持久化", func(s domain.Snapshot) bool {
		return finishedAction(s, action.ID)
	})
	finished := findAction(t, snapshot, action.ID)
	if finished.Status != "succeeded" || finished.Result == nil || finished.Result.ExitCode != 0 || finished.Result.Truncated || finished.Result.EffectUnknown {
		t.Fatalf("真实沙箱执行或归属清理未确认：action=%+v result=%+v", finished, finished.Result)
	}
	var report struct {
		UID                int      `json:"uid"`
		EUID               int      `json:"euid"`
		RootFSReadOnly     bool     `json:"rootfs_readonly"`
		RootWriteSucceeded bool     `json:"root_write_succeeded"`
		RootWriteErrno     int      `json:"root_write_errno"`
		Interfaces         []string `json:"interfaces"`
		HostTCPReachable   bool     `json:"host_tcp_reachable"`
		HostHTTPReachable  bool     `json:"host_http_reachable"`
		HostError          string   `json:"host_error"`
	}
	if err := json.Unmarshal(finished.Result.Stdout, &report); err != nil {
		t.Fatalf("解析真实沙箱输出失败：%v；stdout=%q stderr=%q", err, finished.Result.Stdout, finished.Result.Stderr)
	}
	if report.UID != 65534 || report.EUID != 65534 || !report.RootFSReadOnly || report.RootWriteSucceeded || report.RootWriteErrno == 0 {
		t.Fatalf("非 root 或只读根文件系统约束未成立：%+v", report)
	}
	if !reflect.DeepEqual(report.Interfaces, []string{"lo"}) || report.HostTCPReachable || report.HostHTTPReachable || report.HostError == "" || hostHits.Load() != 0 {
		t.Fatalf("沙箱不应存在非 loopback 接口或直连宿主 fixture：report=%+v hostHits=%d", report, hostHits.Load())
	}
	evidence, ok := persistedResult(snapshot, action.ID)
	if !ok || !bytes.Equal(evidence.Stdout, finished.Result.Stdout) || len(snapshot.Observations) != 1 || finished.RequestHash != action.RequestHash {
		t.Fatal("真实沙箱输出必须保留在动作与关联观察中，原请求 hash 不变", snapshot)
	}
	if snapshot.Thread.Status != "ready" || snapshot.Thread.AutoRun || len(snapshot.Attempts) != 0 {
		t.Fatal("人工沙箱动作结束不应启动模型或改变为暂停", snapshot)
	}
}
