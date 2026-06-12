package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	stdtls "crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	xproxy "golang.org/x/net/proxy"
)

const defaultWASafeServerPublicKeyHex = "8e8c0f74c3ebc5d7a6865c6c3c843856b06121cce8ea774d22fb6f122512302d"

type nativeHTTPClient struct {
	client *http.Client
}

func (c *nativeHTTPClient) CloseIdleConnections() {
	if c == nil || c.client == nil {
		return
	}
	c.client.CloseIdleConnections()
}

func newNativeHTTPClient(proxy string) (*nativeHTTPClient, error) {
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 20 * time.Second}
	transport := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &stdtls.Config{InsecureSkipVerify: true},
		DialContext:       dialer.DialContext,
		DialTLSContext:    nativeAndroidDialTLSContext(dialer.DialContext),
	}
	if proxy != "" {
		parsed, err := parseOutboundProxyURL(proxy)
		if err != nil {
			return nil, err
		}
		if err := configureNativeHTTPProxy(transport, parsed, dialer); err != nil {
			return nil, err
		}
	}
	return &nativeHTTPClient{client: &http.Client{Timeout: 20 * time.Second, Transport: transport}}, nil
}

func configureNativeHTTPProxy(transport *http.Transport, parsed *url.URL, dialer *net.Dialer) error {
	if transport == nil || parsed == nil {
		return nil
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 20 * time.Second}
	}
	switch {
	case parsed.Scheme == "http" || parsed.Scheme == "https":
		transport.Proxy = nil
		transport.DialContext = dialer.DialContext
		transport.DialTLSContext = nativeAndroidDialTLSContext(nativeHTTPProxyConnectDialContext(dialer.DialContext, parsed))
	case strings.HasPrefix(parsed.Scheme, "socks5"):
		var auth *xproxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &xproxy.Auth{User: parsed.User.Username(), Password: password}
		}
		proxyDialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, dialer)
		if err != nil {
			return err
		}
		contextDialer, ok := proxyDialer.(xproxy.ContextDialer)
		if !ok {
			return fmt.Errorf("SOCKS5 proxy dialer does not support context")
		}
		transport.DialContext = contextDialer.DialContext
		transport.DialTLSContext = nativeAndroidDialTLSContext(contextDialer.DialContext)
	default:
		return fmt.Errorf("unsupported HTTP proxy scheme")
	}
	return nil
}

func nativeHTTPProxyConnectDialContext(dialContext func(context.Context, string, string) (net.Conn, error), parsed *url.URL) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		if dialContext == nil {
			return nil, fmt.Errorf("native proxy dialer is not configured")
		}
		if parsed == nil || parsed.Host == "" {
			return nil, fmt.Errorf("HTTP proxy host is required")
		}
		proxyAddress := nativeProxyAddress(parsed)
		conn, err := dialContext(ctx, network, proxyAddress)
		if err != nil {
			return nil, err
		}
		if parsed.Scheme == "https" {
			tlsConn := stdtls.Client(conn, &stdtls.Config{ServerName: parsed.Hostname(), InsecureSkipVerify: true})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			conn = tlsConn
		}
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
		if err := writeNativeHTTPConnect(conn, parsed, addr); err != nil {
			_ = conn.Close()
			return nil, err
		}
		reader := bufio.NewReader(conn)
		statusLine, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		statusLine = strings.TrimSpace(statusLine)
		if !nativeHTTPConnectStatusOK(statusLine) {
			_ = conn.Close()
			return nil, fmt.Errorf("HTTP CONNECT proxy failed")
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		_ = conn.SetDeadline(time.Time{})
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}
}

func nativeProxyAddress(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err == nil {
		return parsed.Host
	}
	port := "80"
	if parsed.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(parsed.Hostname(), port)
}

func writeNativeHTTPConnect(conn net.Conn, parsed *url.URL, target string) error {
	headers := []string{
		"CONNECT " + target + " HTTP/1.1",
		"Host: " + target,
		"Proxy-Connection: Keep-Alive",
	}
	if parsed != nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		credential := parsed.User.Username() + ":" + password
		headers = append(headers, "Proxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	}
	_, err := conn.Write([]byte(strings.Join(headers, "\r\n") + "\r\n\r\n"))
	return err
}

func nativeHTTPConnectStatusOK(statusLine string) bool {
	parts := strings.Fields(statusLine)
	return len(parts) >= 2 && len(parts[1]) == 3 && parts[1][0] == '2'
}

func nativeAndroidDialTLSContext(dialContext func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		if dialContext == nil {
			return nil, fmt.Errorf("native TLS dialer is not configured")
		}
		rawConn, err := dialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		tlsConn := utls.UClient(rawConn, &utls.Config{ServerName: host, InsecureSkipVerify: true}, utls.HelloAndroid_11_OkHttp)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
}

func (c *nativeHTTPClient) postWASafe(ctx context.Context, endpoint string, plain string, userAgent string, attestation nativeSoftwareAttestation) (map[string]any, string, error) {
	if endpoint == "" {
		return nil, "", fmt.Errorf("endpoint is not configured")
	}
	envelope, err := buildWASafeEnvelope([]byte(plain), defaultWASafeServerPublicKeyHex, attestation)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(envelope.Body))
	if err != nil {
		return nil, "", err
	}
	setNativeHTTPHeader(req, "Content-Type", "application/x-www-form-urlencoded")
	setNativeHTTPHeader(req, "User-Agent", firstNonEmpty(userAgent, nativeUserAgent(defaultWAAppVersion)))
	setNativeHTTPHeader(req, "WaMsysRequest", "1")
	if envelope.Authorization != "" {
		setNativeHTTPHeader(req, "Authorization", envelope.Authorization)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, envelope.Enc, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	result := map[string]any{"status_code": float64(resp.StatusCode), "response_text": string(data)}
	var parsed map[string]any
	if json.Unmarshal(data, &parsed) == nil {
		for key, value := range parsed {
			result[key] = value
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, envelope.Enc, fmt.Errorf("wasafe endpoint returned status %d", resp.StatusCode)
	}
	return result, envelope.Enc, nil
}

func setNativeHTTPHeader(req *http.Request, name string, value string) {
	req.Header[name] = []string{value}
}

func encryptWASafe(plaintext []byte, serverPublicKeyHex string) (string, error) {
	serverRaw, err := hex.DecodeString(serverPublicKeyHex)
	if err != nil {
		return "", err
	}
	serverPublic, err := ecdh.X25519().NewPublicKey(serverRaw)
	if err != nil {
		return "", err
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	shared, err := ephemeral.ECDH(serverPublic)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(shared)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, make([]byte, 12), plaintext, nil)
	combined := append(append([]byte{}, ephemeral.PublicKey().Bytes()...), sealed...)
	return b64u(combined), nil
}

func encHash(enc string) string {
	sum := sha256.Sum256([]byte(enc))
	return hex.EncodeToString(sum[:])
}
