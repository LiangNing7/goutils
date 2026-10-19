//nolint:errcheck
package options

import (
	"errors"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/pflag"

	"github.com/LiangNing7/goutils/pkg/log"
)

var _ IOptions = (*HealthOptions)(nil)

// Defaults applied to a zero-valued HealthOptions (for example one decoded from
// configuration) so that the health check server is always safe to start.
const (
	defaultHealthCheckPath         = "/healthz"
	defaultHealthCheckAddress      = "0.0.0.0:20250"
	defaultHealthReadHeaderTimeout = 5 * time.Second
	defaultHealthWriteTimeout      = 10 * time.Second
	defaultHealthIdleTimeout       = 60 * time.Second
)

// HealthOptions defines options for the health check server.
type HealthOptions struct {
	// Enable debugging by exposing profiling information.
	HTTPProfile        bool   `json:"enable-http-profiler" mapstructure:"enable-http-profiler"`
	HealthCheckPath    string `json:"check-path" mapstructure:"check-path"`
	HealthCheckAddress string `json:"check-address" mapstructure:"check-address"`

	// ReadHeaderTimeout/WriteTimeout/IdleTimeout protect the endpoint from slow
	// clients. A bare http.ListenAndServe uses none of them, which makes the
	// server trivially susceptible to slowloris style connections.
	ReadHeaderTimeout time.Duration `json:"read-header-timeout" mapstructure:"read-header-timeout"`
	WriteTimeout      time.Duration `json:"write-timeout" mapstructure:"write-timeout"`
	IdleTimeout       time.Duration `json:"idle-timeout" mapstructure:"idle-timeout"`
}

// NewHealthOptions create a `zero` value instance.
func NewHealthOptions() *HealthOptions {
	return &HealthOptions{
		HTTPProfile:        false,
		HealthCheckPath:    defaultHealthCheckPath,
		HealthCheckAddress: defaultHealthCheckAddress,
		ReadHeaderTimeout:  defaultHealthReadHeaderTimeout,
		WriteTimeout:       defaultHealthWriteTimeout,
		IdleTimeout:        defaultHealthIdleTimeout,
	}
}

// fillDefaults completes the options with sane values. It is safe to call on a
// zero-valued struct.
func (o *HealthOptions) fillDefaults() {
	if o.HealthCheckPath == "" {
		o.HealthCheckPath = defaultHealthCheckPath
	}

	if o.HealthCheckAddress == "" {
		o.HealthCheckAddress = defaultHealthCheckAddress
	}

	if o.ReadHeaderTimeout <= 0 {
		o.ReadHeaderTimeout = defaultHealthReadHeaderTimeout
	}

	if o.WriteTimeout <= 0 {
		o.WriteTimeout = defaultHealthWriteTimeout
	}

	if o.IdleTimeout <= 0 {
		o.IdleTimeout = defaultHealthIdleTimeout
	}
}

// Validate verifies flags passed to HealthOptions. As a side effect it fills in
// the values that were not configured.
func (o *HealthOptions) Validate() []error {
	errs := []error{}

	o.fillDefaults()

	return errs
}

// AddFlags adds flags related to health check for a specific APIServer to the specified FlagSet.
func (o *HealthOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	fs.BoolVar(&o.HTTPProfile, join(prefixes...)+"health.enable-http-profiler", o.HTTPProfile, "Expose runtime profiling data via HTTP.")
	fs.StringVar(&o.HealthCheckPath, join(prefixes...)+"health.check-path", o.HealthCheckPath, "Specifies liveness health check request path.")
	fs.StringVar(&o.HealthCheckAddress, join(prefixes...)+"health.check-address", o.HealthCheckAddress, "Specifies liveness health check bind address.")
}

// router builds the mux router serving the health check (and, when enabled, the
// pprof handlers).
func (o *HealthOptions) router() *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc(o.path(), handler).Methods(http.MethodGet)
	if o.HTTPProfile {
		r.HandleFunc("/debug/pprof/profile", pprof.Profile)
		r.HandleFunc("/debug/pprof/{_:.*}", pprof.Index)
	}

	return r
}

func (o *HealthOptions) path() string {
	if o.HealthCheckPath == "" {
		return defaultHealthCheckPath
	}

	return o.HealthCheckPath
}

func (o *HealthOptions) address() string {
	if o.HealthCheckAddress == "" {
		return defaultHealthCheckAddress
	}

	return o.HealthCheckAddress
}

// newServer builds the health check http.Server with all timeouts applied.
func (o *HealthOptions) newServer() *http.Server {
	o.fillDefaults()

	return &http.Server{
		Addr: o.address(),
		// #nosec G112 -- timeouts below make the server resistant to slowloris.
		Handler:           o.router(),
		ReadTimeout:       o.ReadHeaderTimeout,
		ReadHeaderTimeout: o.ReadHeaderTimeout,
		WriteTimeout:      o.WriteTimeout,
		IdleTimeout:       o.IdleTimeout,
	}
}

// ServeHealthCheckE starts the health check server and blocks until it fails.
//
// It returns the error instead of terminating the process: a busy port used to
// take the whole application down through log.Fatalf inside a goroutine.
func (o *HealthOptions) ServeHealthCheckE() error {
	srv := o.newServer()

	log.Infow("Starting health check server", "path", o.path(), "addr", srv.Addr)

	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

// ServeHealthCheck serves the liveness endpoint. The error is logged but never
// fatal, so a port conflict cannot bring down the whole process.
func (o *HealthOptions) ServeHealthCheck() {
	if err := o.ServeHealthCheckE(); err != nil {
		log.Errorw(err, "Error serving health check endpoint")
	}
}

func handler(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-type", "application/json")
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte(`{"status": "ok"}`))
}
