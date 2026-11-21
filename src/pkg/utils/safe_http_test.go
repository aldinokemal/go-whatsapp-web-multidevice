package utils_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
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
		"http://100.100.100.200/latest/meta-data",
		"http://[::ffff:100.100.100.200]/",
		"http://198.18.0.1/",
		"http://192.0.0.1/",
		"http://240.0.0.1/",
		"http://0.0.0.1/",
		"http://[64:ff9b::a00:1]/",
		"http://[64:ff9b::a9fe:a9fe]/latest/meta-data",
		"http://[64:ff9b:1::808:808]/",
		"http://[::a9fe:a9fe]/",
		"http://[::7f00:1]/",
		"http://[::ffff:0:a9fe:a9fe]/",
		"http://[2002:a9fe:a9fe::1]/",
		"http://[2001:0:808:808::1]/",
		"http://[fec0::1]/",
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

			_, _, fileErr := utils.DownloadFileFromURL(context.Background(), raw)
			require.Error(t, fileErr)
			_, metaErr := utils.GetMetaDataFromURL(raw)
			require.Error(t, metaErr)
			_, _, imageErr := utils.DownloadImageFromURL(context.Background(), raw)
			require.Error(t, imageErr)
			_, _, audioErr := utils.DownloadAudioFromURL(context.Background(), raw)
			require.Error(t, audioErr)
			_, _, videoErr := utils.DownloadVideoFromURL(context.Background(), raw)
			require.Error(t, videoErr)
			require.False(t, dialed)
		})
	}
}

func TestUntrustedFetchAllowsOrdinaryPublicLiteral(t *testing.T) {
	for _, raw := range []string{
		"http://1.1.1.1/status",
		"http://8.8.8.8/status",
		"http://[::ffff:8.8.8.8]/status",
		"http://[64:ff9b::808:808]/status",
		"http://[2002:808:808::1]/status",
	} {
		t.Run(raw, func(t *testing.T) {
			dialed := false
			restore := utils.SetUntrustedFetchTestHooks(nil, func(context.Context, string, string) (net.Conn, error) {
				dialed = true
				return nil, errors.New("stopped after allow")
			}, false)
			t.Cleanup(restore)

			_, _, err := utils.DownloadFileFromURL(context.Background(), raw)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "non-public")
			require.True(t, dialed)
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

	_, _, err := utils.DownloadFileFromURL(context.Background(), "http://internal.example/secret")
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

	_, _, err := utils.DownloadFileFromURL(context.Background(), "http://rebind.example/secret")
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

	body, name, err := utils.DownloadFileFromURL(context.Background(), "http://public.example/file.bin")
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
	_, _, fileErr := utils.DownloadFileFromURL(context.Background(), "http://public.example/doc")
	require.Error(t, fileErr)
	require.Contains(t, fileErr.Error(), "169.254.169.254")
	_, _, audioErr := utils.DownloadAudioFromURL(context.Background(), "http://public.example/a.mp3")
	require.Error(t, audioErr)
	require.Contains(t, audioErr.Error(), "169.254.169.254")
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

func TestUntrustedFetchAllowsOperatorChatwootHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		fmt.Fprint(w, "attachment")
	}))
	t.Cleanup(server.Close)

	prevURL, prevHosts := config.ChatwootURL, config.ChatwootAllowedHosts
	config.ChatwootURL = "http://chatwoot.internal:3000"
	config.ChatwootAllowedHosts = []string{"Storage.Internal"}
	t.Cleanup(func() { config.ChatwootURL, config.ChatwootAllowedHosts = prevURL, prevHosts })

	var dialed []string
	t.Cleanup(utils.SetUntrustedFetchTestHooks(
		func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("192.168.1.20")}, nil
		},
		func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
		false,
	))

	attachment := utils.WithChatwootAttachmentFetch(context.Background())
	_, _, err := utils.DownloadFileFromURL(attachment, "http://chatwoot.internal:3000/rails/active_storage/blobs/report.pdf")
	require.NoError(t, err)
	_, _, err = utils.DownloadImageFromURL(attachment, "http://storage.internal/bucket/photo.png")
	require.NoError(t, err)
	require.Equal(t, []string{"chatwoot.internal:3000", "storage.internal:80"}, dialed)

	_, _, err = utils.DownloadFileFromURL(attachment, "http://other.internal/report.pdf")
	require.ErrorContains(t, err, "192.168.1.20")
	_, _, err = utils.DownloadFileFromURL(attachment, "http://chatwoot.internal:6379/")
	require.ErrorContains(t, err, "192.168.1.20")

	// A caller-selected URL outside the Chatwoot attachment flow gets no exemption.
	_, _, err = utils.DownloadFileFromURL(context.Background(), "http://chatwoot.internal:3000/rails/active_storage/blobs/report.pdf")
	require.ErrorContains(t, err, "192.168.1.20")
	_, _, err = utils.DownloadAudioFromURL(context.Background(), "http://storage.internal:6379/")
	require.ErrorContains(t, err, "192.168.1.20")
}

func TestUntrustedFetchChecksPunycodeFormOfIDNHost(t *testing.T) {
	var looked []string
	var dialed string
	t.Cleanup(utils.SetUntrustedFetchTestHooks(
		func(_ context.Context, host string) ([]net.IP, error) {
			looked = append(looked, host)
			if host == "xn--bcher-kva.de" {
				return []net.IP{net.ParseIP("1.1.1.1")}, nil
			}
			return nil, fmt.Errorf("unexpected host %s", host)
		},
		func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = address
			return nil, errors.New("stopped after allow")
		},
		false,
	))

	_, _, err := utils.DownloadFileFromURL(context.Background(), "http://bücher.de/doc.pdf")
	require.ErrorContains(t, err, "stopped after allow")
	require.Equal(t, []string{"xn--bcher-kva.de", "xn--bcher-kva.de"}, looked)
	require.Equal(t, "1.1.1.1:80", dialed)
}

func TestUntrustedFetchReusesPooledConnections(t *testing.T) {
	t.Cleanup(utils.AllowUntrustedLoopbackForTest())
	var conns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "file")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	for range 5 {
		_, _, err := utils.DownloadFileFromURL(context.Background(), server.URL+"/doc.pdf")
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, conns.Load(), "each fetch must not open its own transport")
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

func TestUntrustedFetchServesOwnStaticsFromDisk(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("statics/media", 0o755))
	require.NoError(t, os.WriteFile("statics/media/a b.pdf", []byte("%PDF-1.4"), 0o644))
	require.NoError(t, os.MkdirAll("storages", 0o755))
	require.NoError(t, os.WriteFile("storages/whatsapp.db", []byte("secret"), 0o644))

	var dialed atomic.Int32
	t.Cleanup(utils.SetUntrustedFetchTestHooks(
		func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("172.18.0.5")}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialed.Add(1)
			return nil, errors.New("unexpected dial")
		},
		false,
	))

	data, name, err := utils.DownloadFileFromURL(context.Background(), "http://gowa:3000/statics/media/a%20b.pdf")
	require.NoError(t, err)
	require.Equal(t, "%PDF-1.4", string(data))
	require.Equal(t, "a%20b.pdf", name)

	for _, target := range []string{
		"http://gowa:3000/statics/../storages/whatsapp.db",
		"http://gowa:3000/statics/media/missing.pdf",
		"http://gowa:3000/app/devices",
		"http://gowa:3000/statics/..%5c..%5cstorages%5cwhatsapp.db",
	} {
		_, _, err = utils.DownloadFileFromURL(context.Background(), target)
		require.ErrorContains(t, err, "non-public address", target)
	}
	require.Zero(t, dialed.Load())
}
