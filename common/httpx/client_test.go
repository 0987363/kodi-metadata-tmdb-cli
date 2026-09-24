package httpx

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvalidProxyNeverConnectsDirectly(t *testing.T) {
	for _, proxyURL := range []string{
		"://invalid",
		"ftp://proxy-user:sensitive-proxy-secret@127.0.0.1:1",
		"http://proxy-user:sensitive-proxy-secret@127.0.0.1:bad",
		"127.0.0.1:1",
	} {
		t.Run(proxyURL, func(t *testing.T) {
			var calls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				io.WriteString(w, "unexpected direct connection")
			}))
			defer target.Close()
			client := NewClient(proxyURL, 1)
			defer client.CloseIdleConnections()
			response, err := client.Get(target.URL)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || calls.Load() != 0 {
				t.Fatalf("非法代理被静默转为直连: error=%v requests=%d", err, calls.Load())
			}
			if strings.Contains(err.Error(), "proxy-user") || strings.Contains(err.Error(), "sensitive-proxy-secret") {
				t.Fatal("代理校验错误泄露凭据")
			}
		})
	}
}

func TestHTTPProxyReceivesRequestsAndEmptyProxyStillConnectsDirectly(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		io.WriteString(w, "direct")
	}))
	defer target.Close()
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.URL.String() != target.URL+"/movie" {
			t.Errorf("代理收到错误目标: %s", r.URL)
		}
		io.WriteString(w, "proxied")
	}))
	defer proxyServer.Close()
	for _, test := range []struct{ proxy, want string }{{proxyServer.URL, "proxied"}, {"", "direct"}, {"  ", "direct"}} {
		client := NewClient(test.proxy, 1)
		defer client.CloseIdleConnections()
		response, err := client.Get(target.URL + "/movie")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != test.want {
			t.Fatalf("代理路由行为变化: %q %v", body, err)
		}
	}
	if directCalls.Load() != 2 || proxyCalls.Load() != 1 {
		t.Fatalf("请求路由错误: direct=%d proxy=%d", directCalls.Load(), proxyCalls.Load())
	}
}

func TestValidateProxyAcceptsSupportedURLsAndRejectsMalformedAddresses(t *testing.T) {
	for _, value := range []string{"", " ", "http://proxy.example", "https://user:p%40ss@proxy.example:8443/", "socks5://proxy.example", "socks5h://proxy.example:1080", "http://[::1]:8080", "socks5://[fe80::1%25en0]:1080"} {
		if err := ValidateProxy(value); err != nil {
			t.Errorf("合法代理被拒绝: %v", err)
		}
	}
	for _, value := range []string{"http:///", "socks5://", "http://:8080", "http://proxy.example:65536", "http://::1", "socks5://[not-ip]:1080", "https://proxy.example/path", "http://proxy.example?token=secret", "http://proxy.example#fragment"} {
		if err := ValidateProxy(value); err == nil {
			t.Errorf("非法代理被接受: %s", value)
		}
	}
}

func TestSOCKSStalledHandshakeClosesConnection(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		for _, throughHTTP := range []bool{false, true} {
			name := scheme + "/dial-context"
			if throughHTTP {
				name = scheme + "/http-request"
			}
			t.Run(name, func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				closed := make(chan error, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					conn, err := listener.Accept()
					if err != nil {
						closed <- err
						return
					}
					defer conn.Close()
					if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
						closed <- err
						return
					}
					_, err = io.Copy(io.Discard, conn)
					closed <- err
				}()
				t.Cleanup(func() { listener.Close(); <-done })
				var directCalls atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					directCalls.Add(1)
				}))
				defer target.Close()
				timeout := 0
				if throughHTTP {
					timeout = 1
				}
				client := NewClient(scheme+"://proxy-user:sensitive-proxy-secret@"+listener.Addr().String(), timeout)
				defer client.CloseIdleConnections()
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				started := time.Now()
				if throughHTTP {
					request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
					if requestErr != nil {
						t.Fatal(requestErr)
					}
					response, requestErr := client.Do(request)
					if response != nil {
						response.Body.Close()
					}
					err = requestErr
				} else {
					conn, dialErr := client.Transport.(*http.Transport).DialContext(ctx, "tcp", strings.TrimPrefix(target.URL, "http://"))
					if conn != nil {
						conn.Close()
					}
					err = dialErr
				}
				if err == nil || time.Since(started) > time.Second || directCalls.Load() != 0 {
					t.Fatalf("握手未及时取消或转为直连：error=%v elapsed=%v direct=%d", err, time.Since(started), directCalls.Load())
				}
				if strings.Contains(err.Error(), "proxy-user") || strings.Contains(err.Error(), "sensitive-proxy-secret") {
					t.Fatal("代理连接错误泄露凭据")
				}
				select {
				case closeErr := <-closed:
					if closeErr != nil {
						t.Fatalf("代理握手连接未主动关闭：%v", closeErr)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("HTTP 请求已超时，SOCKS 握手仍持有连接")
				}
			})
		}
	}
}

func TestSOCKSProxyCarriesHTTPWithoutDirectConnection(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			var directCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				directCalls.Add(1)
			}))
			defer target.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- serveSOCKSHTTP(listener, strings.TrimPrefix(target.URL, "http://")) }()
			t.Cleanup(func() {
				listener.Close()
				if err := <-done; err != nil {
					t.Error(err)
				}
			})
			client := NewClient(scheme+"://"+listener.Addr().String(), 1)
			defer client.CloseIdleConnections()
			response, err := client.Get(target.URL + "/movie")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "proxied" || directCalls.Load() != 0 {
				t.Fatalf("SOCKS 请求路由错误：%q %v direct=%d", body, err, directCalls.Load())
			}
		})
	}
}

func serveSOCKSHTTP(listener net.Listener, target string) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return err
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	if greeting[0] != 5 || len(methods) != 1 || methods[0] != 0 {
		return fmt.Errorf("SOCKS 握手错误：%v %v", greeting, methods)
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return err
	}
	connect := make([]byte, 10)
	if _, err := io.ReadFull(conn, connect); err != nil {
		return err
	}
	address := net.JoinHostPort(net.IP(connect[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(connect[8:]))))
	if connect[0] != 5 || connect[1] != 1 || connect[2] != 0 || connect[3] != 1 || address != target {
		return fmt.Errorf("SOCKS 目标错误：%v %s", connect, address)
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return err
	}
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return err
	}
	defer request.Body.Close()
	if request.Method != http.MethodGet || request.URL.Path != "/movie" {
		return fmt.Errorf("SOCKS 内 HTTP 请求错误：%s %s", request.Method, request.URL)
	}
	_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nConnection: close\r\n\r\nproxied")
	return err
}
