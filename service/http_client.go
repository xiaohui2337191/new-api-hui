package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	utls "github.com/refraction-networking/utls"

	"golang.org/x/net/proxy"
)

var (
	httpClient      *http.Client
	proxyClientLock sync.Mutex
	proxyClients    = make(map[string]*http.Client)
	airforceClient  *http.Client
)

func checkRedirect(req *http.Request, via []*http.Request) error {
	fetchSetting := system_setting.GetFetchSetting()
	urlStr := req.URL.String()
	if err := common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func InitHttpClient() {
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment, // Support HTTP_PROXY, HTTPS_PROXY, NO_PROXY env vars
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}

	if common.RelayTimeout == 0 {
		httpClient = &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
	} else {
		httpClient = &http.Client{
			Transport:     transport,
			Timeout:       time.Duration(common.RelayTimeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
	}

	// AirForce专用客户端不需要在InitHttpClient中初始化
	// 因为GetAirforceClient会每次创建新的客户端实现TLS指纹随机化
}

func GetHttpClient() *http.Client {
	return httpClient
}

// 可用的 uTLS Client Hello ID 列表（模拟不同浏览器）
var utlsHelloIDs = []utls.ClientHelloID{
	utls.HelloChrome_Auto,
	utls.HelloFirefox_Auto,
	utls.HelloSafari_Auto,
	utls.HelloEdge_Auto,
	utls.HelloIOS_Auto,
}

// getRandomUTLSHelloID 获取随机的 Client Hello ID
func getRandomUTLSHelloID() utls.ClientHelloID {
	return utlsHelloIDs[time.Now().UnixNano()%int64(len(utlsHelloIDs))]
}

// GetAirforceClient 获取 AirForce 专用客户端
// 每次调用都返回新的客户端，确保 TLS 指纹随机化
func GetAirforceClient() *http.Client {
	// 每次返回全新的客户端，确保完全隔离（无连接复用、无TLS会话复用）
	transport := &http.Transport{
		DisableCompression:  true,
		DisableKeepAlives:   true,
		MaxIdleConns:        0,
		MaxIdleConnsPerHost: 0,
		IdleConnTimeout:     1 * time.Nanosecond,
		Proxy:               http.ProxyFromEnvironment,
	}

	// 使用 uTLS 进行 TLS 指纹随机化
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// 先通过代理建立连接
		var conn net.Conn
		var err error
		
		// 获取代理设置
		proxyURL, _ := http.ProxyFromEnvironment(nil)
		if proxyURL != nil {
			// 通过代理连接
			switch proxyURL.Scheme {
			case "http", "https":
				// HTTP 代理的 CONNECT 方法
				conn, err = dialThroughHTTPProxy(ctx, network, addr, proxyURL)
			case "socks5", "socks5h":
				conn, err = dialThroughSocks5Proxy(ctx, network, addr, proxyURL)
			default:
				conn, err = net.Dial(network, addr)
			}
		} else {
			conn, err = net.Dial(network, addr)
		}
		
		if err != nil {
			return nil, err
		}

		// 提取主机名（移除端口）
		host := addr
		for i := len(addr) - 1; i >= 0; i-- {
			if addr[i] == ':' {
				host = addr[:i]
				break
			}
		}

		config := &utls.Config{
			ServerName:         host,
			InsecureSkipVerify: common.TLSInsecureSkipVerify,
		}

		uConn := utls.UClient(conn, config, getRandomUTLSHelloID())
		if err := uConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}

		return uConn, nil
	}

	return &http.Client{
		Transport:     transport,
		Timeout:       time.Duration(common.RelayTimeout) * time.Second,
		CheckRedirect: checkRedirect,
	}
}

// dialThroughHTTPProxy 通过 HTTP 代理建立连接
func dialThroughHTTPProxy(ctx context.Context, network, addr string, proxyURL *url.URL) (net.Conn, error) {
	// 建立到代理的连接
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		if proxyURL.Scheme == "https" {
			proxyAddr = proxyURL.Host + ":443"
		} else {
			proxyAddr = proxyURL.Host + ":80"
		}
	}
	
	conn, err := net.Dial(network, proxyAddr)
	if err != nil {
		return nil, err
	}

	// 发送 CONNECT 请求
	connectReq := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n\r\n"
	_, err = conn.Write([]byte(connectReq))
	if err != nil {
		conn.Close()
		return nil, err
	}

	// 读取响应
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// 检查响应
	resp := string(buf[:n])
	if len(resp) < 12 || resp[9:12] != "200" {
		conn.Close()
		return nil, fmt.Errorf("proxy connect failed: %s", resp)
	}

	return conn, nil
}

// dialThroughSocks5Proxy 通过 SOCKS5 代理建立连接
func dialThroughSocks5Proxy(ctx context.Context, network, addr string, proxyURL *url.URL) (net.Conn, error) {
	// 获取认证信息
	var auth *proxy.Auth
	if proxyURL.User != nil {
		auth = &proxy.Auth{
			User:     proxyURL.User.Username(),
			Password: "",
		}
		if password, ok := proxyURL.User.Password(); ok {
			auth.Password = password
		}
	}

	// 创建 SOCKS5 拨号器
	dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}

	return dialer.Dial(network, addr)
}

// GetHttpClientWithProxy returns the default client or a proxy-enabled one when proxyURL is provided.
func GetHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return GetHttpClient(), nil
	}
	return NewProxyHttpClient(proxyURL)
}

// ResetProxyClientCache 清空代理客户端缓存，确保下次使用时重新初始化
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	proxyClients = make(map[string]*http.Client)
}

// NewProxyHttpClient 创建支持代理的 HTTP 客户端
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}

	proxyClientLock.Lock()
	if client, ok := proxyClients[proxyURL]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch parsedURL.Scheme {
	case "http", "https":
		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyURL(parsedURL),
		}
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}
		client := &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	case "socks5", "socks5h":
		// 获取认证信息
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		// 创建 SOCKS5 代理拨号器
		// proxy.SOCKS5 使用 tcp 参数，所有 TCP 连接包括 DNS 查询都将通过代理进行。行为与 socks5h 相同
		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}

		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}

		client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}
}