package httpx

import (
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// ValidateProxy 校验显式代理地址；空值保留默认网络设置，错误不包含代理凭据。
func ValidateProxy(value string) error {
	_, err := parseProxyURL(value)
	return err
}

func parseProxyURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("代理地址不是有效 URL")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("代理协议必须为 http、https、socks5 或 socks5h")
	}
	if parsed.Hostname() == "" || parsed.Opaque != "" {
		return nil, errors.New("代理地址必须包含主机")
	}
	if strings.Contains(parsed.Hostname(), ":") {
		if _, err := netip.ParseAddr(parsed.Hostname()); err != nil || !strings.HasPrefix(parsed.Host, "[") {
			return nil, errors.New("代理 IPv6 地址必须使用有效的方括号格式")
		}
	}
	if port := parsed.Port(); port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return nil, errors.New("代理端口必须在 0 到 65535 之间")
		}
	}
	if parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("代理地址不允许包含路径、查询参数或片段")
	}
	return parsed, nil
}

type invalidProxyTransport struct {
	err error
}

func (t invalidProxyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}
