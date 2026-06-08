package app

import "strings"

func (e *NativeEngine) httpForProxy() (*nativeHTTPClient, error) {
	if _, err := e.proxyURL(); err != nil {
		return nil, err
	}
	return e.http, nil
}

func (e *NativeEngine) proxyURL() (string, error) {
	if proxyURL := strings.TrimSpace(e.activeProxyURL); proxyURL != "" {
		return proxyURL, nil
	}
	return "", nil
}
