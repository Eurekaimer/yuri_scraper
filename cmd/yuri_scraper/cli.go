package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/app"
	"github.com/Eurekaimer/yuri_scraper/internal/buildinfo"
	"github.com/Eurekaimer/yuri_scraper/internal/diagnostics"
	"github.com/Eurekaimer/yuri_scraper/internal/install"
	"github.com/Eurekaimer/yuri_scraper/internal/library"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/search/sites"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	"github.com/Eurekaimer/yuri_scraper/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

type commandOptions struct {
	config, outputDir, source, baseURL, title, format, binDir, root string
	concurrency, retryCount                                         int
	rateLimit                                                       float64
	json, yes, help, debug                                          bool
}

// Run executes a command. Result data goes to stdout; diagnostics and progress
// go to stderr. Help and version never initialize the application or filesystem.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) (exitCode int) {
	command, arguments, err := selectCommand(args)
	if err != nil {
		return commandError(stderr, err, 2)
	}
	if command == "help" {
		if len(arguments) == 0 {
			printRootHelp(stdout)
			return 0
		}
		if len(arguments) != 1 || !knownCommand(arguments[0]) {
			return commandError(stderr, errors.New("help expects one command name"), 2)
		}
		command, arguments = arguments[0], []string{"--help"}
	}
	if command == "version" {
		if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h" || arguments[0] == "-help") {
			fmt.Fprintln(stdout, "Usage: yuri-scraper version")
			return 0
		}
		if len(arguments) != 0 {
			return commandError(stderr, errors.New("version does not accept arguments"), 2)
		}
		fmt.Fprintln(stdout, buildinfo.FullVersion(Version))
		return 0
	}
	options, positional, visited, flags, err := parseOptions(command, arguments)
	if err != nil {
		return commandError(stderr, err, 2)
	}
	if options.help {
		if command == "tui" {
			printRootHelp(stdout)
		}
		fmt.Fprintf(stdout, "Usage: yuri-scraper %s\n", commandUsage(command))
		flags.SetOutput(stdout)
		flags.PrintDefaults()
		return 0
	}
	if err := validateArguments(command, positional, options); err != nil {
		return commandError(stderr, err, 2)
	}
	if visited["output-dir"] && strings.TrimSpace(options.outputDir) == "" {
		return commandError(stderr, errors.New("--output-dir cannot be empty"), 2)
	}
	if command == "install" && visited["output-dir"] {
		return commandError(stderr, errors.New("install uses output_dir from its configuration; --output-dir applies to application commands"), 2)
	}
	if ctx.Err() != nil {
		return commandError(stderr, ctx.Err(), 130)
	}
	if command == "tui" && !term.IsTerminal(os.Stdin.Fd()) {
		return commandError(stderr, errors.New("the interactive interface requires a terminal; use search, download, or library list"), 2)
	}

	if command == "install" {
		return runInstall(ctx, options, stdout, stderr)
	}
	configPath, _, err := store.ResolvePaths(options.config, ProjectRoot)
	if err != nil {
		return commandError(stderr, err, 1)
	}
	if _, err := os.Stat(configPath); err != nil {
		return commandError(stderr, fmt.Errorf("configuration unavailable at %s: %w; use --config FILE or reinstall with install --root DIR", configPath, err), 1)
	}
	cfg, err := store.LoadConfig(configPath)
	if err != nil {
		return commandError(stderr, fmt.Errorf("load configuration %s: %w", configPath, err), 1)
	}
	if !filepath.IsAbs(cfg.OutputDir) {
		cfg.OutputDir = filepath.Join(filepath.Dir(configPath), cfg.OutputDir)
	}
	if visited["output-dir"] {
		cfg.OutputDir, err = filepath.Abs(options.outputDir)
		if err != nil {
			return commandError(stderr, err, 2)
		}
	}
	if visited["format"] {
		cfg.Format = options.format
	}
	if visited["concurrency"] {
		cfg.Concurrency = options.concurrency
	}
	if visited["rate-limit"] {
		cfg.RateLimit = options.rateLimit
	}
	if visited["retry-count"] {
		cfg.RetryCount = options.retryCount
	}
	if err := cfg.Validate(); err != nil {
		return commandError(stderr, err, 2)
	}
	if command == "config" {
		if options.json {
			return encodeResult(stdout, stderr, cfg)
		}
		fmt.Fprintf(stdout, "Config: %s\nOutput: %s\nFormat: %s\nConcurrency: %d\nRate limit: %g\nRetries: %d\n", configPath, cfg.OutputDir, cfg.Format, cfg.Concurrency, cfg.RateLimit, cfg.RetryCount)
		return 0
	}
	if command == "logs" {
		text, err := diagnostics.Recent(cfg.OutputDir)
		if err != nil {
			return commandError(stderr, fmt.Errorf("读取日志 %s: %w", diagnostics.Path(cfg.OutputDir), err), 1)
		}
		fmt.Fprintln(stderr, "日志:", diagnostics.Path(cfg.OutputDir))
		fmt.Fprintln(stdout, text)
		return 0
	}
	// Logs never share stdout with machine-readable results or the TUI.
	previousLogger, previousLogOutput := slog.Default(), log.Writer()
	interactive := command == "tui" || (command == "library" && len(positional) == 0 && !options.json && term.IsTerminal(os.Stdin.Fd()))
	var logOutput io.Writer = stderr
	if interactive {
		logOutput = io.Discard
	}
	logFile, logErr := diagnostics.Open(cfg.OutputDir)
	if logErr != nil {
		fmt.Fprintf(stderr, "警告：无法写入诊断日志 %s：%v；程序继续运行。\n", diagnostics.Path(cfg.OutputDir), logErr)
	} else {
		logOutput = logFile
		defer logFile.Close()
	}
	slog.SetDefault(diagnostics.Logger(logOutput, options.debug))
	defer func() { slog.SetDefault(previousLogger); log.SetOutput(previousLogOutput) }()
	slog.Info("session.start", "command", command, "version", buildinfo.VersionLabel(Version), "debug", options.debug)
	defer func() { slog.Info("session.end", "command", command, "exit_code", exitCode) }()
	service, err := app.NewWithBaseURL(cfg, options.source, options.baseURL)
	if err != nil {
		return commandError(stderr, err, failureCode(ctx))
	}
	switch command {
	case "tui":
		return runTUI(ctx, service, false, stdout, stderr)
	case "search", "query":
		if err := authenticate(ctx, service, options.source, stderr); err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		results, err := service.Search.SearchSite(ctx, options.source, strings.Join(positional, " "))
		if err != nil {
			slog.Error("search.failed", "source", options.source, diagnostics.Failure(err))
		} else {
			slog.Info("search.completed", "source", options.source, "results", len(results))
		}
		if err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		if results == nil {
			results = []search.SearchResult{}
		}
		if options.json {
			return encodeResult(stdout, stderr, results)
		}
		for _, book := range results {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", book.Title, book.Author, book.SiteName, book.BookURL)
		}
		return 0
	case "download":
		if err := authenticate(ctx, service, options.source, stderr); err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		book, err := service.ResolveBook(ctx, options.source, positional[0], options.title)
		if err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		updates, err := service.Start(ctx, book, search.ChapterPlanOptions{MaxScanPages: cfg.Yamibo.MaxScanPages, MinPostChars: cfg.Yamibo.MinPostChars})
		if err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		return consumeTask(updates, options.json, stdout, stderr)
	case "library", "list", "open", "reveal", "retry", "remove", "forget":
		if command == "library" && len(positional) == 0 && !options.json && term.IsTerminal(os.Stdin.Fd()) {
			return runTUI(ctx, service, true, stdout, stderr)
		}
		return runLibrary(ctx, service, command, positional, options, stdout, stderr)
	}
	return commandError(stderr, errors.New("unsupported command"), 2)
}

func runTUI(ctx context.Context, service *app.Service, showLibrary bool, stdout, stderr io.Writer) int {
	// Run owns diagnostic logging for both front ends.
	model := tui.NewModel(service, Version)
	if showLibrary {
		model.ShowLibrary()
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(stdout))
	_, runErr := program.Run()
	service.Cancel()
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := service.Wait(cleanup); err != nil {
		return commandError(stderr, fmt.Errorf("wait for canceled task: %w", err), 1)
	}
	if runErr != nil {
		return commandError(stderr, runErr, failureCode(ctx))
	}
	return 0
}

func knownCommand(command string) bool {
	switch command {
	case "help", "version", "tui", "search", "query", "download", "library", "list", "open", "reveal", "retry", "remove", "forget", "config", "install", "logs":
		return true
	}
	return false
}

func selectCommand(args []string) (string, []string, error) {
	var prefix []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h", "-help":
			return "help", args[index+1:], nil
		case "--version", "-version":
			return "version", args[index+1:], nil
		}
		if !strings.HasPrefix(arg, "-") {
			if !knownCommand(arg) {
				return "", nil, fmt.Errorf("unknown command %q", arg)
			}
			return arg, append(prefix, args[index+1:]...), nil
		}
		if arg == "--debug" || strings.HasPrefix(arg, "--debug=") {
			prefix = append(prefix, arg)
			continue
		}
		name := strings.SplitN(strings.TrimLeft(arg, "-"), "=", 2)[0]
		if name != "config" && name != "output-dir" {
			return "", nil, fmt.Errorf("unknown global option %q", arg)
		}
		prefix = append(prefix, arg)
		if !strings.Contains(arg, "=") {
			index++
			if index == len(args) {
				return "", nil, fmt.Errorf("option %s requires a value", arg)
			}
			prefix = append(prefix, args[index])
		}
	}
	return "tui", prefix, nil
}

func parseOptions(command string, args []string) (commandOptions, []string, map[string]bool, *flag.FlagSet, error) {
	var options commandOptions
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.config, "config", "", "Configuration file (or YURI_SCRAPER_CONFIG)")
	flags.StringVar(&options.outputDir, "output-dir", "", "Override the output directory")
	flags.BoolVar(&options.help, "help", false, "Show command help without side effects")
	flags.BoolVar(&options.help, "h", false, "Show command help")
	flags.BoolVar(&options.debug, "debug", false, "记录 52 书库请求耗时、状态和阶段，不记录凭据或搜索词")
	switch command {
	case "search", "query", "download", "tui":
		flags.StringVar(&options.source, "source", "52shuku", "Source: 52shuku/52书库 or yamibo/百合会")
		flags.StringVar(&options.baseURL, "base-url", "", "Local HTTP fixture origin (52shuku only)")
	}
	switch command {
	case "search", "query", "download", "library", "list", "retry", "config", "install":
		flags.BoolVar(&options.json, "json", false, "Write machine-readable JSON result to stdout")
	}
	if command == "download" {
		flags.StringVar(&options.title, "title", "", "Book title override (otherwise source metadata)")
		flags.StringVar(&options.format, "format", "", "Output format: epub or txt")
		flags.IntVar(&options.concurrency, "concurrency", 0, "Maximum parallel chapter downloads")
		flags.Float64Var(&options.rateLimit, "rate-limit", 0, "Maximum requests per second")
		flags.IntVar(&options.retryCount, "retry-count", 0, "Retry count for transient errors")
	}
	if command == "library" || command == "remove" || command == "forget" {
		flags.BoolVar(&options.yes, "yes", false, "Confirm remove/forget without prompting")
	}
	if command == "install" {
		flags.StringVar(&options.binDir, "bin-dir", "", "User command directory (default ~/.local/bin)")
		flags.StringVar(&options.root, "root", "", "Absolute or relative project checkout path")
	}
	// The standard flag package stops at the first positional argument. Collect
	// flags separately to permit both `download URL --json` and flags-first use.
	var flagArgs, positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name := strings.SplitN(strings.TrimLeft(arg, "-"), "=", 2)[0]
		option := flags.Lookup(name)
		if option == nil {
			return options, nil, nil, flags, fmt.Errorf("unknown option %q for %s", arg, command)
		}
		flagArgs = append(flagArgs, arg)
		boolean, isBoolean := option.Value.(interface{ IsBoolFlag() bool })
		if !strings.Contains(arg, "=") && !(isBoolean && boolean.IsBoolFlag()) {
			index++
			if index == len(args) {
				return options, nil, nil, flags, fmt.Errorf("option %s requires a value", arg)
			}
			flagArgs = append(flagArgs, args[index])
		}
	}
	if err := flags.Parse(flagArgs); err != nil {
		return options, nil, nil, flags, err
	}
	visited := make(map[string]bool)
	flags.Visit(func(option *flag.Flag) { visited[option.Name] = true })
	if options.source != "" {
		switch strings.ToLower(options.source) {
		case "52shuku", sites.Source52Shuku:
			options.source = sites.Source52Shuku
		case "yamibo", sites.SourceYamibo:
			options.source = sites.SourceYamibo
		default:
			if !options.help {
				return options, nil, nil, flags, fmt.Errorf("unknown source %q", options.source)
			}
		}
	}
	return options, positional, visited, flags, nil
}

func validateArguments(command string, args []string, options commandOptions) error {
	switch command {
	case "tui", "install", "list", "logs":
		if len(args) != 0 {
			return fmt.Errorf("%s does not accept positional arguments", command)
		}
	case "search", "query":
		if len(args) == 0 || strings.TrimSpace(strings.Join(args, " ")) == "" {
			return errors.New("search requires a query")
		}
	case "download", "open", "reveal", "retry", "remove", "forget":
		target := "library ID"
		if command == "download" {
			target = "URL"
		}
		if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
			return fmt.Errorf("%s requires exactly one %s", command, target)
		}
	case "config":
		if len(args) != 1 || args[0] != "show" {
			return errors.New("config expects the show subcommand")
		}
	case "library":
		if len(args) == 0 {
			return nil
		}
		action := args[0]
		if action != "list" && action != "open" && action != "reveal" && action != "retry" && action != "remove" && action != "forget" {
			return fmt.Errorf("unknown library action %q", action)
		}
		return validateArguments(action, args[1:], options)
	}
	if (command == "remove" || command == "forget") && !options.yes {
		return fmt.Errorf("%s requires --yes", command)
	}
	if options.baseURL != "" && options.source != sites.Source52Shuku {
		return errors.New("--base-url is supported only for 52shuku local fixtures")
	}
	return nil
}

func runLibrary(ctx context.Context, service *app.Service, command string, args []string, options commandOptions, stdout, stderr io.Writer) int {
	if command == "library" {
		command = "list"
		if len(args) > 0 {
			command, args = args[0], args[1:]
		}
	}
	entries, err := service.Library()
	if err != nil {
		return commandError(stderr, err, failureCode(ctx))
	}
	if command == "list" {
		if entries == nil {
			entries = []library.Entry{}
		}
		if options.json {
			return encodeResult(stdout, stderr, entries)
		}
		for _, entry := range entries {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\tmissing=%t\n", entry.ID, entry.Title, entry.State, entry.FilePath, entry.Missing)
		}
		return 0
	}
	entry, err := library.Find(entries, args[0])
	if err != nil {
		return commandError(stderr, err, 1)
	}
	switch command {
	case "open":
		err = library.Open(entry)
	case "reveal":
		err = library.Reveal(entry)
	case "remove":
		err = service.Remove(entry.ID)
	case "forget":
		err = service.RemoveRecord(entry.ID)
	case "retry":
		if entry.BookURL == "" || entry.SiteName == "" {
			return commandError(stderr, errors.New("this file has no source metadata to retry"), 1)
		}
		if err := authenticate(ctx, service, entry.SiteName, stderr); err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		updates, err := service.Retry(ctx, entry.ID)
		if err != nil {
			return commandError(stderr, err, failureCode(ctx))
		}
		return consumeTask(updates, options.json, stdout, stderr)
	}
	if err != nil {
		return commandError(stderr, err, failureCode(ctx))
	}
	if options.json {
		return encodeResult(stdout, stderr, map[string]string{"action": command, "id": entry.ID})
	}
	fmt.Fprintf(stdout, "%s\t%s\n", command, entry.ID)
	return 0
}

func consumeTask(updates <-chan app.Snapshot, asJSON bool, stdout, stderr io.Writer) int {
	var final app.Snapshot
	for snapshot := range updates {
		final = snapshot
		fmt.Fprintf(stderr, "%s: %d/%d completed, %d failed\n", snapshot.State, snapshot.Completed, snapshot.Total, snapshot.Failed)
	}
	var code int
	switch final.State {
	case "completed":
		code = 0
	case "partial":
		code = 3
	case "canceled":
		code = 130
	case "failed":
		code = 1
	default:
		return commandError(stderr, errors.New("task ended without a terminal result"), 1)
	}
	if asJSON {
		if encoded := encodeResult(stdout, stderr, final); encoded != 0 {
			return encoded
		}
	} else {
		if final.OutputFile != "" {
			fmt.Fprintln(stdout, final.OutputFile)
		}
		if final.FailedFile != "" {
			fmt.Fprintln(stdout, final.FailedFile)
		}
	}
	if final.Error != "" {
		fmt.Fprintln(stderr, final.Error)
	}
	return code
}

func authenticate(ctx context.Context, service *app.Service, source string, stderr io.Writer) error {
	adapter := service.Search.Registry().Get(source)
	if adapter == nil {
		return fmt.Errorf("unknown source %q", source)
	}
	auth, requiresAuth := adapter.(search.CredentialsAuthenticator)
	if !requiresAuth || !auth.RequiresAuthentication() || auth.IsAuthenticated() {
		return nil
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		return errors.New("this source requires authentication; run in a terminal to enter credentials securely")
	}
	ttyPath := "/dev/tty"
	if runtime.GOOS == "windows" {
		ttyPath = "CONIN$"
	}
	tty, err := os.OpenFile(ttyPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open credential terminal: %w", err)
	}
	defer tty.Close()
	fmt.Fprint(stderr, "Account: ")
	var account strings.Builder
	var character [1]byte
	for {
		if _, err := io.ReadFull(tty, character[:]); err != nil {
			return err
		}
		if character[0] == '\n' {
			break
		}
		if character[0] != '\r' {
			account.WriteByte(character[0])
		}
	}
	fmt.Fprint(stderr, "Password: ")
	password, err := term.ReadPassword(tty.Fd())
	fmt.Fprintln(stderr)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer func() {
		for index := range password {
			password[index] = 0
		}
	}()
	return auth.Login(ctx, strings.TrimSpace(account.String()), string(password))
}

func runInstall(ctx context.Context, options commandOptions, stdout, stderr io.Writer) int {
	config := options.config
	root := options.root
	if root == "" {
		root = ProjectRoot
		if root == "" {
			root = "."
		}
	}
	if config == "" {
		config = os.Getenv("YURI_SCRAPER_CONFIG")
	}
	result, err := install.Install(ctx, install.Options{Root: root, BinDir: options.binDir, ConfigPath: config, Version: Version})
	if err != nil {
		return commandError(stderr, err, failureCode(ctx))
	}
	if options.json {
		if code := encodeResult(stdout, stderr, result); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "Installed: %s\nConfig: %s\n", result.Binary, result.Config)
	}
	if directoryOnPath(result.BinDir) {
		fmt.Fprintf(stderr, "%s is already on PATH in this session; run yuri-scraper from any directory.\n", result.BinDir)
	} else {
		fmt.Fprintf(stderr, "Add %s to PATH, then run yuri-scraper from any directory. No shell files were modified.\n", result.BinDir)
	}
	return 0
}

// directoryOnPath reports whether a directory is already searched by PATH in the
// current environment, so the installer can state the truth instead of guessing.
func directoryOnPath(dir string) bool {
	target := filepath.Clean(dir)
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		if filepath.Clean(entry) == target {
			return true
		}
	}
	return false
}

func encodeResult(stdout, stderr io.Writer, value any) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		return commandError(stderr, fmt.Errorf("write JSON result: %w", err), 1)
	}
	return 0
}
func commandError(stderr io.Writer, err error, code int) int {
	fmt.Fprintln(stderr, "Error:", err)
	fmt.Fprintln(stderr, "建议:", diagnostics.Guidance(err))
	return code
}
func failureCode(ctx context.Context) int {
	if ctx.Err() != nil {
		return 130
	}
	return 1
}

func printRootHelp(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: yuri-scraper [--config FILE] [--output-dir DIR] [--debug] COMMAND [OPTIONS]\n\nWithout a command, opens the terminal interface.\nCommands: tui, search (query), download, library, list, open, reveal, retry, remove, forget, config show, install, logs, version\nlogs: 查看最近 100 条诊断日志；--debug: 开启请求级诊断。\nUse COMMAND --help for command options.\nExit codes: 0 success, 1 failure, 2 usage, 3 partial download, 130 canceled.")
}
func commandUsage(command string) string {
	switch command {
	case "search", "query":
		return command + " QUERY [--source NAME] [--json] [OPTIONS]"
	case "download":
		return "download URL [--title TITLE] [--source NAME] [--json] [OPTIONS]"
	case "library":
		return "library [list | open ID | reveal ID | retry ID | remove ID --yes | forget ID --yes] [--json] [OPTIONS]"
	case "open", "reveal", "retry":
		return command + " ID [OPTIONS]"
	case "remove", "forget":
		return command + " ID --yes [OPTIONS]"
	case "config":
		return "config show [--json] [OPTIONS]"
	default:
		return command + " [OPTIONS]"
	}
}
