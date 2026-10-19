package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	_ "go.uber.org/automaxprocs"
	"k8s.io/component-base/cli"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/term"

	"github.com/LiangNing7/goutils/pkg/log"
	genericoptions "github.com/LiangNing7/goutils/pkg/options"
	"github.com/LiangNing7/goutils/pkg/version"
)

// App is the main structure of a cli application.
// It is recommended that an app be created with the app.NewApp() function.
type App struct {
	name        string
	shortDesc   string
	description string
	run         RunFunc
	runWithCtx  RunFuncWithContext
	cmd         *cobra.Command
	args        cobra.PositionalArgs

	// +optional
	healthCheckFunc HealthCheckFunc

	// +optional
	options any

	// +optional
	silence bool

	// +optional
	noConfig bool

	// watching and re-reading config files
	// +optional
	watch bool

	contextExtractors map[string]func(context.Context) string
}

// RunFunc defines the application's startup callback function.
type RunFunc func() error

// RunFuncWithContext defines the application's startup callback function
// that receives the command context, so cancellation and deadlines can be
// propagated to the server for a graceful shutdown.
type RunFuncWithContext func(ctx context.Context) error

// HealthCheckFunc defines the health check function for the application.
type HealthCheckFunc func() error

// Option defines optional parameters for initializing the application
// structure.
type Option func(*App)

// WithOptions to open the application's function to read from the command line
// or read parameters from the configuration file.
func WithOptions(opts any) Option {
	return func(app *App) {
		app.options = opts
	}
}

// WithRunFunc is used to set the application startup callback function option.
func WithRunFunc(run RunFunc) Option {
	return func(app *App) {
		app.run = run
	}
}

// WithRunFuncWithContext is used to set the application startup callback
// function option, with the command context passed in.
func WithRunFuncWithContext(run RunFuncWithContext) Option {
	return func(app *App) {
		app.runWithCtx = run
	}
}

// WithDescription is used to set the description of the application.
func WithDescription(desc string) Option {
	return func(app *App) {
		app.description = desc
	}
}

// WithHealthCheckFunc is used to set the health check function for the application.
// The app framework will use the function to start a health check server.
func WithHealthCheckFunc(fn HealthCheckFunc) Option {
	return func(app *App) {
		app.healthCheckFunc = fn
	}
}

// WithDefaultHealthCheckFunc set the default health check function.
func WithDefaultHealthCheckFunc() Option {
	fn := func() HealthCheckFunc {
		return func() error {
			go func() {
				defer func() {
					// 后台协程 panic 会直接崩溃进程，这里兜底记录.
					if r := recover(); r != nil {
						log.Errorw(fmt.Errorf("panic: %v", r), "Recovered from panic in health check server")
					}
				}()

				genericoptions.NewHealthOptions().ServeHealthCheck()
			}()

			return nil
		}
	}

	return WithHealthCheckFunc(fn())
}

// WithSilence sets the application to silent mode, in which the program startup
// information, configuration information, and version information are not
// printed in the console.
func WithSilence() Option {
	return func(app *App) {
		app.silence = true
	}
}

// WithNoConfig set the application does not provide config flag.
func WithNoConfig() Option {
	return func(app *App) {
		app.noConfig = true
	}
}

// WithValidArgs set the validation function to valid non-flag arguments.
func WithValidArgs(args cobra.PositionalArgs) Option {
	return func(app *App) {
		app.args = args
	}
}

// WithDefaultValidArgs set default validation function to valid non-flag arguments.
func WithDefaultValidArgs() Option {
	return func(app *App) {
		app.args = cobra.NoArgs
	}
}

// WithWatchConfig watching and re-reading config files.
func WithWatchConfig() Option {
	return func(app *App) {
		app.watch = true
	}
}

func WithLoggerContextExtractor(contextExtractors map[string]func(context.Context) string) Option {
	return func(app *App) {
		app.contextExtractors = contextExtractors
	}
}

// NewApp creates a new application instance based on the given application name,
// binary name, and other options.
func NewApp(name string, shortDesc string, opts ...Option) *App {
	app := &App{
		name:      name,
		run:       func() error { return nil },
		shortDesc: shortDesc,
	}

	for _, o := range opts {
		o(app)
	}

	app.buildCommand()

	return app
}

// buildCommand is used to build a cobra command.
func (app *App) buildCommand() {
	cmd := &cobra.Command{
		Use:   formatBaseName(app.name),
		Short: app.shortDesc,
		Long:  app.description,
		RunE:  app.runCommand,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return nil
		},
		Args: app.args,
	}
	// When error printing is enabled for the Cobra command, a flag parse
	// error gets printed first, then optionally the often long usage
	// text. This is very unreadable in a console because the last few
	// lines that will be visible on screen don't include the error.
	//
	// The recommendation from #sig-cli was to print the usage text, then
	// the error. We implement this consistently for all commands here.
	// However, we don't want to print the usage text when command
	// execution fails for other reasons than parsing. We detect this via
	// the FlagParseError callback.
	//
	// Some commands, like kubectl, already deal with this themselves.
	// We don't change the behavior for those.
	if !cmd.SilenceUsage {
		cmd.SilenceUsage = true
		cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
			// Re-enable usage printing.
			c.SilenceUsage = false
			return err
		})
	}
	// In all cases error printing is done below.
	cmd.SilenceErrors = true

	cmd.SetOutput(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.Flags().SortFlags = true

	var (
		fs  *pflag.FlagSet
		fss *cliflag.NamedFlagSets
	)
	switch typed := app.options.(type) {
	case NamedFlagSetOptions:
		// 先取用户返回的 NamedFlagSets，再在其上取 "global" FlagSet.
		// 顺序反了会导致 fs 指向一个不属于命令的孤儿 FlagSet，
		// 使 --config/-c、--version 无法被解析.
		named := typed.Flags()
		fss = &named
		fs = fss.FlagSet("global")
	case FlagSetOptions:
		fs = cmd.PersistentFlags()
		typed.AddFlags(fs)
	default:
		// 未提供 options 时也必须有一个非 nil 的 FlagSet，
		// 否则 version.AddFlags(fs) 会对 nil map 赋值而 panic.
		fs = cmd.PersistentFlags()
	}

	version.AddFlags(fs)

	if !app.noConfig {
		addConfigFlag(fs, app.name, app.watch, app.options)
	}

	if fss != nil {
		// 必须在上面的 version/config flag 注册完成后再 AddFlagSet，
		// 因为这些 flag 是直接加到 fss 的 "global" 集合上的.
		for _, f := range fss.FlagSets {
			cmd.Flags().AddFlagSet(f)
		}

		cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
		cliflag.SetUsageAndHelpFunc(cmd, *fss, cols)
	}

	app.cmd = cmd
}

// Run is used to launch the application.
//
// 它会安装 SIGINT/SIGTERM 处理、在退出前刷新日志，然后以命令的退出码退出进程.
func (app *App) Run() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code := app.RunContext(ctx)

	// 文件 sink 可能带缓冲，退出前刷新，避免丢日志.
	log.Sync()
	os.Exit(code)
}

// RunContext 使用给定的 context 运行应用并返回退出码，不会调用 os.Exit.
// context 会被传递给 RunFuncWithContext，从而实现优雅退出.
func (app *App) RunContext(ctx context.Context) int {
	app.cmd.SetContext(ctx)

	return cli.Run(app.cmd)
}

func (app *App) runCommand(cmd *cobra.Command, args []string) error {
	// display application version information
	version.PrintAndExitIfRequested()

	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		return err
	}

	if app.options != nil {
		if err := viper.Unmarshal(app.options); err != nil {
			return err
		}

		if complete, ok := app.options.(interface{ Complete() error }); ok {
			if err := complete.Complete(); err != nil {
				return err
			}
		}

		if validate, ok := app.options.(interface{ Validate() error }); ok {
			if err := validate.Validate(); err != nil {
				return err
			}
		}
	}

	app.initializeLogger()

	if !app.silence {
		log.Infow("Starting application", "name", app.name, "version", version.Get().ToJSON())
		log.Infow("Golang settings", "GOGC", os.Getenv("GOGC"), "GOMAXPROCS", os.Getenv("GOMAXPROCS"), "GOTRACEBACK", os.Getenv("GOTRACEBACK"))
		if !app.noConfig {
			PrintConfig()
		} else if app.options != nil {
			cliflag.PrintFlags(cmd.Flags())
		}
	}

	if app.healthCheckFunc != nil {
		if err := app.healthCheckFunc(); err != nil {
			return err
		}
	}

	// run application
	if app.runWithCtx != nil {
		return app.runWithCtx(cmd.Context())
	}

	return app.run()
}

// Command returns cobra command instance inside the application.
func (app *App) Command() *cobra.Command {
	return app.cmd
}

// formatBaseName is formatted as an executable file name under different
// operating systems according to the given name.
func formatBaseName(name string) string {
	// Make case-insensitive and strip executable suffix if present
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
		name = strings.TrimSuffix(name, ".exe")
	}
	return name
}

// initializeLogger sets up the logging system based on the configuration.
func (app *App) initializeLogger() {
	logOptions := log.NewOptions()

	// Configure logging options from viper
	if viper.IsSet("log.disable-caller") {
		logOptions.DisableCaller = viper.GetBool("log.disable-caller")
	}
	if viper.IsSet("log.disable-stacktrace") {
		logOptions.DisableStacktrace = viper.GetBool("log.disable-stacktrace")
	}
	if viper.IsSet("log.level") {
		logOptions.Level = viper.GetString("log.level")
	}
	if viper.IsSet("log.format") {
		logOptions.Format = viper.GetString("log.format")
	}
	if viper.IsSet("log.output-paths") {
		logOptions.OutputPaths = viper.GetStringSlice("log.output-paths")
	}

	// Initialize logging with custom context extractors
	log.Init(logOptions, log.WithContextExtractor(app.contextExtractors))
}
