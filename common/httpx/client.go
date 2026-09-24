package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// NewClient 创建可选代理的 http client
func NewClient(proxyConnect string, timeoutSeconds int) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: transport}
	if timeoutSeconds > 0 {
		client.Timeout = time.Duration(timeoutSeconds) * time.Second
	}

	proxyURL, err := parseProxyURL(proxyConnect)
	if err != nil {
		client.Transport = invalidProxyTransport{err: err}
		return client
	}
	if proxyURL == nil {
		return client
	}

	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		dialTimeout := 30 * time.Second
		if timeoutSeconds > 0 {
			dialTimeout = time.Duration(timeoutSeconds) * time.Second
		}
		dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			dialer := &net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: 30 * time.Second,
			}
			proxyDialer, err := proxy.FromURL(proxyURL, dialer)
			if err != nil {
				return nil, errors.New("SOCKS 代理初始化失败")
			}
			contextDialer, ok := proxyDialer.(proxy.ContextDialer)
			if !ok {
				return nil, errors.New("SOCKS 代理不支持可取消连接")
			}
			return contextDialer.DialContext(ctx, network, addr)
		}
		transport.DialContext = dialContext
	}

	return client
}
