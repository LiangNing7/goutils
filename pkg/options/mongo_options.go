package options

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var _ IOptions = (*MongoOptions)(nil)

// defaultMongoTimeout is used for connect/ping when Timeout is not configured
// (or is non-positive). It must be strictly positive: context.WithTimeout with a
// zero or negative duration yields an already-expired context, which makes every
// Connect/Ping call fail immediately.
const defaultMongoTimeout = 30 * time.Second

// MongoOptions contains options for connecting to a MongoDB server.
type MongoOptions struct {
	URL        string        `json:"url" mapstructure:"url"`
	Database   string        `json:"database" mapstructure:"database"`
	Collection string        `json:"collection" mapstructure:"collection"`
	Username   string        `json:"username" mapstructure:"username"`
	Password   string        `json:"-" mapstructure:"password"`
	Timeout    time.Duration `json:"timeout" mapstructure:"timeout"`
	TLSOptions *TLSOptions   `json:"tls" mapstructure:"tls"`
}

// NewMongoOptions create a `zero` value instance.
func NewMongoOptions() *MongoOptions {
	return &MongoOptions{
		Timeout:    30 * time.Second,
		TLSOptions: NewTLSOptions(),
	}
}

// validateMongoURL reports whether uri is an acceptable MongoDB connection
// string.
//
// net/url.Parse must not be used here: it accepts `""` and arbitrary text (so
// the driver silently fell back to mongodb://localhost:27017) and it rejects the
// perfectly valid `127.0.0.1:27017` form. Instead we hand the URI to the driver
// itself through the stable public ClientOptions API, which records the parse
// error and surfaces it via Validate().
func validateMongoURL(uri string) error {
	if strings.TrimSpace(uri) == "" {
		return fmt.Errorf("--mongo.url can not be empty")
	}

	if err := options.Client().ApplyURI(uri).Validate(); err != nil {
		return fmt.Errorf("invalid mongo url %q: %w", uri, err)
	}

	return nil
}

// effectiveTimeout returns the timeout to use for connect/ping operations.
func (o *MongoOptions) effectiveTimeout() time.Duration {
	if o.Timeout <= 0 {
		return defaultMongoTimeout
	}

	return o.Timeout
}

// Validate verifies flags passed to MongoOptions.
func (o *MongoOptions) Validate() []error {
	errs := []error{}

	if err := validateMongoURL(o.URL); err != nil {
		errs = append(errs, err)
	}

	if o.Database == "" {
		errs = append(errs, fmt.Errorf("--mongo.database can not be empty"))
	}

	if o.Collection == "" {
		errs = append(errs, fmt.Errorf("--mongo.collection can not be empty"))
	}

	if o.TLSOptions != nil {
		errs = append(errs, o.TLSOptions.Validate()...)
	}

	return errs
}

// AddFlags adds flags related to mongodb storage for a specific APIServer to the specified FlagSet.
func (o *MongoOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	if o.TLSOptions == nil {
		o.TLSOptions = NewTLSOptions()
	}
	o.TLSOptions.AddFlags(fs, "mongo")

	fs.DurationVar(&o.Timeout, "mongo.timeout", o.Timeout, "Timeout is the maximum amount of time a dial will wait for a connect to complete.")
	fs.StringVar(&o.URL, "mongo.url", o.URL, "The MongoDB server address.")
	fs.StringVar(&o.Database, "mongo.database", o.Database, "The MongoDB database name.")
	fs.StringVar(&o.Collection, "mongo.collection", o.Collection, "The MongoDB collection name.")
	fs.StringVar(&o.Username, "mongo.username", o.Username, "Username of the MongoDB database (optional).")
	fs.StringVar(&o.Password, "mongo.password", o.Password, "Password of the MongoDB database (optional).")
}

// NewClient creates a new MongoDB client based on the provided options.
func (o *MongoOptions) NewClient() (*mongo.Client, error) {
	// Fail fast on an empty/invalid URL: ApplyURI("") makes the driver fall back
	// to mongodb://localhost:27017, so production would silently connect to the
	// local machine.
	if err := validateMongoURL(o.URL); err != nil {
		return nil, err
	}

	// Set client options
	opts := options.Client().ApplyURI(o.URL).SetReadPreference(readpref.Primary())
	if o.Timeout > 0 {
		opts.SetConnectTimeout(o.Timeout).SetSocketTimeout(o.Timeout).SetServerSelectionTimeout(o.Timeout)
	}

	if o.Username != "" || o.Password != "" {
		opts.SetAuth(options.Credential{
			AuthSource: o.Database,
			Username:   o.Username,
			Password:   o.Password,
		})
	}

	if o.TLSOptions != nil {
		tlsConf, err := o.TLSOptions.TLSConfig()
		if err != nil {
			return nil, err
		}
		opts.SetTLSConfig(tlsConf)
	}

	// o.effectiveTimeout() never returns a non-positive value, so the context
	// below is always usable (context.WithTimeout(ctx, 0) would already be
	// expired by the time mongo.Connect/Ping run).
	ctx, cancel := context.WithTimeout(context.Background(), o.effectiveTimeout())
	defer cancel()

	// Connect to MongoDB
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Ping the MongoDB server to check the connection
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, err
	}

	return client, nil
}
