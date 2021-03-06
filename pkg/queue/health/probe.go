/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package health

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	network "knative.dev/networking/pkg"
)

// HTTPProbeConfigOptions holds the HTTP probe config options
type HTTPProbeConfigOptions struct {
	Timeout time.Duration
	*corev1.HTTPGetAction
	KubeMajor string
	KubeMinor string
}

// TCPProbeConfigOptions holds the TCP probe config options
type TCPProbeConfigOptions struct {
	SocketTimeout time.Duration
	Address       string
}

// TCPProbe checks that a TCP socket to the address can be opened.
// Did not reuse k8s.io/kubernetes/pkg/probe/tcp to not create a dependency
// on klog.
func TCPProbe(config TCPProbeConfigOptions) error {
	conn, err := net.DialTimeout("tcp", config.Address, config.SocketTimeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

var transport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	//nolint:gosec // We explicitly don't need to check certs here.
	t.TLSClientConfig.InsecureSkipVerify = true
	return t
}()

// HTTPProbe checks that HTTP connection can be established to the address.
func HTTPProbe(config HTTPProbeConfigOptions) error {
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}

	url := url.URL{
		Scheme: string(config.Scheme),
		Host:   net.JoinHostPort(config.Host, config.Port.String()),
		Path:   config.Path,
	}
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return fmt.Errorf("error constructing probe request %w", err)
	}

	req.Header.Add(network.UserAgentKey, network.KubeProbeUAPrefix+config.KubeMajor+"/"+config.KubeMinor)

	for _, header := range config.HTTPHeaders {
		req.Header.Add(header.Name, header.Value)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		// Ensure body is both read _and_ closed so it can be reused for keep-alive.
		// No point handling errors, connection just won't be reused.
		io.Copy(ioutil.Discard, res.Body)
		res.Body.Close()
	}()

	if !IsHTTPProbeReady(res) {
		return fmt.Errorf("HTTP probe did not respond Ready, got status code: %d", res.StatusCode)
	}

	return nil
}

// IsHTTPProbeReady checks whether we received a successful Response
func IsHTTPProbeReady(res *http.Response) bool {
	// response status code between 200-399 indicates success
	return res.StatusCode >= 200 && res.StatusCode < 400
}

// IsHTTPProbeShuttingDown checks whether the Response indicates the prober is shutting down.
func IsHTTPProbeShuttingDown(res *http.Response) bool {
	// status 410 (Gone) indicates the probe returned a shutdown scenario.
	return res.StatusCode == http.StatusGone
}
