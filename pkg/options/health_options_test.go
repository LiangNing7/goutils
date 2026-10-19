package options

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthOptions_Router(t *testing.T) {
	o := NewHealthOptions()
	srv := httptest.NewServer(o.router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"ok"}`, string(body))

	// pprof must stay off unless explicitly enabled.
	pprofResp, err := http.Get(srv.URL + "/debug/pprof/")
	require.NoError(t, err)
	defer pprofResp.Body.Close()
	require.Equal(t, http.StatusNotFound, pprofResp.StatusCode)
}

func TestHealthOptions_Router_WithProfile(t *testing.T) {
	o := NewHealthOptions()
	o.HTTPProfile = true

	srv := httptest.NewServer(o.router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/pprof/")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestHealthOptions_ZeroValue_ServerTimeouts guards against the bare
// http.ListenAndServe call that had no Read/Write/Idle timeouts (slowloris).
func TestHealthOptions_ZeroValue_ServerTimeouts(t *testing.T) {
	o := &HealthOptions{}

	srv := o.newServer()
	require.NotNil(t, srv)
	require.NotNil(t, srv.Handler)
	require.NotEmpty(t, srv.Addr)
	require.Greater(t, srv.ReadHeaderTimeout, time.Duration(0))
	require.Greater(t, srv.WriteTimeout, time.Duration(0))
	require.Greater(t, srv.IdleTimeout, time.Duration(0))
}

func TestHealthOptions_Validate_FillsTimeoutDefaults(t *testing.T) {
	o := &HealthOptions{}

	require.NotPanics(t, func() { _ = o.Validate() })
	require.Greater(t, o.ReadHeaderTimeout, time.Duration(0))
	require.Greater(t, o.WriteTimeout, time.Duration(0))
	require.Greater(t, o.IdleTimeout, time.Duration(0))
}

// TestHealthOptions_ServeHealthCheckReturnsErrorInsteadOfExiting makes sure a
// bind failure is reported instead of killing the whole process via log.Fatalf.
func TestHealthOptions_ServeHealthCheckReturnsErrorInsteadOfExiting(t *testing.T) {
	o := NewHealthOptions()
	o.HealthCheckAddress = "256.256.256.256:1" // guaranteed to fail to bind

	require.NotPanics(t, func() {
		require.Error(t, o.ServeHealthCheckE())
	})

	// The legacy entry point must not call os.Exit either.
	require.NotPanics(t, func() { o.ServeHealthCheck() })
}
