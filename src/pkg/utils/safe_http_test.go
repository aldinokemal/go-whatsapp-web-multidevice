package utils_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestUntrustedFetchBlocksPrivateAndNonHTTPDestinations(t *testing.T) {
	targets := []string{
		"http://127.0.0.1/",
		"http://127.0.0.1:9/secret",
		"http://localhost/secret",
		"http://169.254.169.254/latest/meta-data",
		"http://169.254.1.1/",
		"http://10.1.2.3/doc",
		"http://192.168.1.20/doc",
		"http://172.16.0.5/doc",
		"http://0.0.0.0/",
		"http://[::1]/",
		"http://[::]/",
		"http://[fd00::1]/",
		"http://[fe80::1]/",
		"http://[::ffff:127.0.0.1]/",
		"http://[::ffff:169.254.169.254]/latest/meta-data",
		"http://224.0.0.1/",
		"http://255.255.255.255/",
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://example.com/",
	}
	for _, raw := range targets {
		t.Run(raw, func(t *testing.T) {
			dialed := false
			restore := utils.SetUntrustedFetchTestHooks(nil, func(context.Context, string, string) (net.Conn, error) {
				dialed = true
				return nil, errors.New("dialed")
			}, false)
			t.Cleanup(restore)

			_, _, fileErr := utils.DownloadFileFromURL(raw)
			require.Error(t, fileErr)
			_, metaErr := utils.GetMetaDataFromURL(raw)
			require.Error(t, metaErr)
			require.False(t, dialed)
		})
	}
}

func TestUntrustedFetchBlocksNameThatResolvesPrivate(t *testing.T) {
	dialed := false
	restore := utils.SetUntrustedFetchTestHooks(
		func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("10.0.0.8")}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("dialed")
		},
		false,
	)
	t.Cleanup(restore)

	_, _, err := utils.DownloadFileFromURL("http://internal.example/secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "10.0.0.8")
	require.False(t, dialed)
}

func TestUntrustedDialRechecksAddressAfterInitialLookup(t *testing.T) {
	calls := 0
	dialed := false
	restore := utils.SetUntrustedFetchTestHooks(
		func(context.Context, string) ([]net.IP, error) {
			calls++
			if calls == 1 {
				return []net.IP{net.ParseIP("1.1.1.1")}, nil
			}
			return []net.IP{net.ParseIP("10.9.8.7")}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("should not dial")
		},
		false,
	)
	t.Cleanup(restore)

	_, _, err := utils.DownloadFileFromURL("http://rebind.example/secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "10.9.8.7")
	require.False(t, dialed)
	require.GreaterOrEqual(t, calls, 2)
}

func TestUntrustedFetchAllowsPublicURLWithStubbedDialer(t *testing.T) {
	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		fmt.Fprint(w, `<html><head><title>Public</title></head></html>`)
	}))
	t.Cleanup(server.Close)

	var dialed []string
	restore := utils.SetUntrustedFetchTestHooks(
		func(_ context.Context, host string) ([]net.IP, error) {
			if host == "public.example" {
				return []net.IP{net.ParseIP("1.1.1.1")}, nil
			}
			return nil, fmt.Errorf("unexpected host %s", host)
		},
		func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if !strings.HasPrefix(address, "1.1.1.1:") {
				return nil, fmt.Errorf("unexpected dial %s", address)
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
		false,
	)
	t.Cleanup(restore)

	meta, err := utils.GetMetaDataFromURL("http://public.example/page")
	require.NoError(t, err)
	require.Equal(t, "Public", meta.Title)
	require.NotEmpty(t, dialed)
	require.True(t, strings.HasPrefix(dialed[0], "1.1.1.1:"))
	require.Equal(t, "public.example", gotHost)

	body, name, err := utils.DownloadFileFromURL("http://public.example/file.bin")
	require.NoError(t, err)
	require.Equal(t, "file.bin", name)
	require.Contains(t, string(body), "Public")
}

func TestUntrustedFetchBlocksRedirectToPrivateAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	var dialed []string
	restore := stubPublicDialer(t, server, &dialed)
	t.Cleanup(restore)

	_, err := utils.GetMetaDataFromURL("http://public.example/page")
	require.Error(t, err)
	require.Contains(t, err.Error(), "169.254.169.254")
	_, _, fileErr := utils.DownloadFileFromURL("http://public.example/doc")
	require.Error(t, fileErr)
	require.Contains(t, fileErr.Error(), "169.254.169.254")
	for _, address := range dialed {
		require.NotContains(t, address, "169.254")
		require.NotContains(t, address, "127.")
	}
}

func TestPreviewImageURLCannotTargetMetadataAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Page</title><meta property="og:image" content="http://169.254.169.254/latest/meta-data/x.jpg"></head></html>`)
	}))
	t.Cleanup(server.Close)

	var dialed []string
	t.Cleanup(stubPublicDialer(t, server, &dialed))

	meta, err := utils.GetMetaDataFromURL("http://public.example/page")
	require.NoError(t, err)
	require.Equal(t, "Page", meta.Title)
	require.Contains(t, meta.Image, "169.254.169.254")
	require.Empty(t, meta.ImageThumb)
	for _, address := range dialed {
		require.NotContains(t, address, "169.254")
		require.NotContains(t, address, "127.")
	}
}

func TestPreviewImageRedirectCannotTargetPrivateAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img.jpg" {
			http.Redirect(w, r, "http://10.2.3.4/secret.jpg", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<html><head><title>Redirected image</title><meta property="og:image" content="http://public.example/img.jpg"></head></html>`)
	}))
	t.Cleanup(server.Close)

	var dialed []string
	t.Cleanup(stubPublicDialer(t, server, &dialed))

	meta, err := utils.GetMetaDataFromURL("http://public.example/page")
	require.NoError(t, err)
	require.Equal(t, "Redirected image", meta.Title)
	require.Empty(t, meta.ImageThumb)
	for _, address := range dialed {
		require.NotContains(t, address, "10.2.3.4")
		require.NotContains(t, address, "127.")
	}
}

func stubPublicDialer(t *testing.T, server *httptest.Server, dialed *[]string) func() {
	t.Helper()
	return utils.SetUntrustedFetchTestHooks(
		func(_ context.Context, host string) ([]net.IP, error) {
			if host == "public.example" {
				return []net.IP{net.ParseIP("1.1.1.1")}, nil
			}
			return nil, fmt.Errorf("unexpected host %s", host)
		},
		func(ctx context.Context, network, address string) (net.Conn, error) {
			*dialed = append(*dialed, address)
			if !strings.HasPrefix(address, "1.1.1.1:") {
				return nil, fmt.Errorf("unexpected dial %s", address)
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
		false,
	)
}
