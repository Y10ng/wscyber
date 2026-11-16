package execution

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"wscyber/internal/domain"
)

// NormalizeOrigin 只接受 scheme/host/port，不把路径、用户信息或通配符隐式降格成授权。
// DNS 名使用 ASCII（国际化名称须提供 punycode），默认端口、末尾点与 IP 文字表示规范化。
func NormalizeOrigin(raw string) (string, error) {
	u, err := parseHTTPURL(raw)
	if err != nil {
		return "", err
	}
	if u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery {
		return "", invalid("origin 只能包含协议、主机和端口，不能包含路径或查询")
	}
	return u.Scheme + "://" + u.Host, nil
}

func parseHTTPURL(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > 16384 || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\t\\#") {
		return nil, invalid("HTTP 地址为空、过长或含有空白、反斜杠、片段")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, invalid("HTTP 地址格式无效")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.Host == "" || u.User != nil {
		return nil, invalid("仅支持没有内嵌凭据的绝对 HTTP/HTTPS 地址")
	}
	host, err := canonicalHost(u.Hostname())
	if err != nil {
		return nil, err
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return nil, invalid("端口不能为空")
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, invalid("端口必须介于 1 与 65535 之间")
		}
		port = strconv.Itoa(n)
		if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
			port = ""
		}
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	return u, nil
}

func canonicalHost(raw string) (string, error) {
	if raw == "" || strings.ContainsAny(raw, "%* \t\r\n") {
		return "", invalid("主机不能为空或包含通配符、空白、IPv6 zone")
	}
	host := strings.ToLower(strings.TrimSuffix(raw, "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String(), nil
	}
	if len(host) > 253 {
		return "", invalid("DNS 名称过长")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", invalid("DNS 名称格式无效")
		}
		for _, c := range label {
			if !asciiAlphaNumeric(c) && c != '-' {
				return "", invalid("DNS 名称须为 ASCII；国际化域名请使用 punycode")
			}
		}
	}
	return host, nil
}

func (e *Executor) validateHTTP(input domain.ActionInput, origins []string) (*url.URL, error) {
	if input.Command != "" {
		return nil, invalid("HTTP 动作不能携带本地命令")
	}
	u, err := parseHTTPURL(input.URL)
	if err != nil {
		return nil, err
	}
	origin := u.Scheme + "://" + u.Host
	allowed := false
	for _, raw := range origins {
		normalized, err := NormalizeOrigin(raw)
		if err != nil {
			return nil, invalid("授权列表包含无效 origin")
		}
		if normalized == origin {
			allowed = true
		}
	}
	if !allowed {
		return nil, invalid("HTTP 地址不在精确授权 origin 内")
	}
	if metadataHost(u.Hostname()) {
		return nil, invalid("不允许访问云元数据服务")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		if err := e.allowedIP(ip); err != nil {
			return nil, err
		}
	}
	method := input.Method
	if method == "" {
		method = "GET"
	}
	if len(method) > 64 || !httpToken(method) || strings.EqualFold(method, "CONNECT") {
		return nil, invalid("HTTP 方法无效或试图建立 CONNECT 隧道")
	}
	if len(input.Body) > maxRequestBytes {
		return nil, invalid("HTTP 请求体不能超过 1 MiB")
	}
	size := 0
	seen := make(map[string]bool, len(input.Headers))
	for name, value := range input.Headers {
		lower := strings.ToLower(name)
		if !httpToken(name) || seen[lower] || forbiddenHeaders[lower] {
			return nil, invalid("HTTP 请求头重复、无效或试图改变路由/连接边界")
		}
		seen[lower] = true
		for i := 0; i < len(value); i++ {
			if value[i] < 32 && value[i] != '\t' || value[i] == 127 {
				return nil, invalid("HTTP 请求头值包含控制字符")
			}
		}
		size += len(name) + len(value) + 4
		if size > maxHeaderBytes {
			return nil, invalid("HTTP 请求头不能超过 64 KiB")
		}
	}
	return u, nil
}

var forbiddenHeaders = map[string]bool{
	"host": true, "connection": true, "proxy-connection": true,
	"proxy-authorization": true, "proxy-authenticate": true,
	"keep-alive": true, "transfer-encoding": true, "te": true,
	"trailer": true, "upgrade": true, "content-length": true,
}

func httpToken(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if !asciiAlphaNumeric(c) && !strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			return false
		}
	}
	return true
}

func metadataHost(host string) bool {
	switch host {
	case "metadata.google.internal", "metadata.goog", "instance-data", "instance-data.ec2.internal":
		return true
	}
	return false
}

// 私网地址本身不构成禁止条件，但调用者必须先通过精确 origin 授权。
// 拒绝 IPv6 转换网段，避免本机/网络翻译规则将表面公网地址转换到受限 IPv4。
var blockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("192.0.0.192/32"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001::/32"),
}

func (e *Executor) allowedIP(ip netip.Addr) error {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return invalid("禁止访问 unspecified、link-local 或 multicast 地址")
	}
	if ip.IsLoopback() {
		if e.config.AllowLoopback {
			return nil
		}
		return invalid("回环地址仅限显式启用 AllowLoopback 的开发环境")
	}
	if !ip.IsGlobalUnicast() {
		return invalid("目标不是可授权的单播地址")
	}
	for _, prefix := range blockedNetworks {
		if prefix.Contains(ip) {
			return invalid("禁止访问云元数据或特殊转换地址")
		}
	}
	return nil
}

func (e *Executor) resolve(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap(), e.allowedIP(ip)
	}
	ips, err := e.lookup(ctx, host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("解析目标地址失败：%w", err)
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("目标没有可连接 IP")
	}
	for _, ip := range ips {
		if err := e.allowedIP(ip); err != nil {
			return netip.Addr{}, err
		}
	}
	// 一次动作只选择一个已校验地址，不用自动重试掩盖未知发送效果。
	return ips[0].Unmap(), nil
}
