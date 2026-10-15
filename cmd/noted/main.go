// Command noted is a self-hosted notes and calendar gRPC service with
// optional AI features.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // zones must work in scratch/distroless images

	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/apple"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/config"
	"github.com/liliang-cn/noted/internal/cortexsync"
	"github.com/liliang-cn/noted/internal/export"
	"github.com/liliang-cn/noted/internal/mcphttp"
	"github.com/liliang-cn/noted/internal/mcpserver"
	"github.com/liliang-cn/noted/internal/plan"
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/server"
	"github.com/liliang-cn/noted/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

var version = "dev"

const usage = `noted - self-hosted notes and calendar over gRPC

Usage:
  noted serve [-config noted.toml]          run the server
  noted user add <name> [-config ...]       create a user and print its first token
  noted user list [-config ...]
  noted user pro <name> [on|off] [-config ...]   grant Noted Pro outside the App Store
  noted token new <user> [-label text]      issue another token for an existing user
  noted export <user> [-o file.zip]         write everything a user has (JSON, Markdown notes, .ics) to a zip
  noted cortex sync <user> [--dry-run] [--full]   send a user's items to the [cortexdb] brain now
  noted mcp [-addr host:port] [-token T]    MCP server on stdio, backed by a running noted server
  noted health [-addr host:port] [-tls]    exit 0 if a server answers (for container healthchecks)
  noted version

Configuration comes from the TOML file (default ./noted.toml when present) and
NOTED_* environment variables; see noted.example.toml.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "user":
		err = userCmd(os.Args[2:])
	case "token":
		err = tokenCmd(os.Args[2:])
	case "export":
		err = exportCmd(os.Args[2:])
	case "cortex":
		err = cortexCmd(os.Args[2:])
	case "mcp":
		err = mcpCmd(os.Args[2:])
	case "health":
		err = health(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "noted:", err)
		os.Exit(1)
	}
}

func configFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("NOTED_CONFIG")
	if def == "" {
		if _, err := os.Stat("noted.toml"); err == nil {
			def = "noted.toml"
		}
	}
	return fs.String("config", def, "path to the TOML config file")
}

func serve(argv []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	path := configFlag(fs)
	if err := fs.Parse(argv); err != nil {
		return err
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	st.SetLocation(cfg.Location()) // the day a repeating to-do falls on next

	authn, err := auth.New(ctx, st, cfg.Auth.Disabled)
	if err != nil {
		return err
	}
	if !cfg.Auth.Disabled {
		users, err := st.ListUsers(ctx)
		if err != nil {
			return err
		}
		if len(users) == 0 {
			slog.Warn("no users yet: create one with `noted user add <name>`")
		}
	} else {
		slog.Warn("auth is DISABLED: every caller acts as user \"default\"")
	}

	deps := server.Deps{
		Store: st, Auth: authn, Hub: reminder.NewHub(), Location: cfg.Location(), Locale: plan.Locale(cfg.Language),
		Reflection: cfg.Server.Reflection,
	}
	if cfg.Auth.Apple.BundleID != "" {
		deps.Apple = &apple.Verifier{BundleID: cfg.Auth.Apple.BundleID}
		deps.AppleSignup = cfg.Auth.Apple.Signup
	}
	deps.EmailSignup = cfg.Auth.Signup
	deps.Account = server.NewAccount(deps) // shared by gRPC and the web portal

	if cfg.AI.Enabled {
		eng, err := ai.New(ctx, ai.Options{
			Store: st, LLM: cfg.AI.LLM, Embed: cfg.AI.Embedding, Dir: cfg.AIDir(), Location: cfg.Location(), Locale: plan.Locale(cfg.Language),
		})
		if err != nil {
			return fmt.Errorf("start AI: %w", err)
		}
		defer func() { _ = eng.Close() }()
		deps.Engine = eng
		s := eng.Status()
		slog.Info("AI enabled", "model", s.Model, "semantic_search", s.Semantic)
		if s.Semantic {
			ix := ai.NewIndexer(st, eng)
			deps.Changed = ix.Wake
			go ix.Run(ctx)
			ix.Wake()
		}
	} else {
		slog.Info("AI disabled: notes and calendar only")
	}

	sch := &reminder.Scheduler{Store: st, Hub: deps.Hub, WebhookURL: cfg.Notify.WebhookURL}
	go sch.Run(ctx)

	if cfg.Cortex.Enabled() {
		sy, err := newCortexSyncer(cfg, st, true)
		if err != nil {
			return err
		}
		defer func() { _ = sy.Brain.(*cortexsync.Remote).Close() }()
		slog.Info("cortexdb sync on", "remote", cfg.Cortex.Remote, "users", len(cfg.Cortex.SyncUsers), "every", cfg.Cortex.Every(),
			"token", cfg.Cortex.Token() != "")
		go sy.Run(ctx)
	}

	var opts []grpc.ServerOption
	if cfg.Server.TLSCert != "" {
		creds, err := credentials.NewServerTLSFromFile(cfg.Server.TLSCert, cfg.Server.TLSKey)
		if err != nil {
			return fmt.Errorf("tls: %w", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}
	g := server.New(deps, opts...)

	lis, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return err
	}
	slog.Info("noted listening", "addr", lis.Addr().String(), "version", version, "data", cfg.Storage.DataDir,
		"tls", cfg.Server.TLSCert != "")

	done := make(chan error, 2)
	go func() { done <- g.Serve(lis) }()

	var hs *http.Server
	if cfg.Server.MCPListen != "" {
		conn, err := localConn(lis.Addr(), cfg.Server.TLSCert != "")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		hs = &http.Server{
			Handler: mcphttp.New(mcphttp.Options{
				Auth: authn, Conn: conn, Location: cfg.Location(), Version: version, Account: deps.Account,
				PublicURL: cfg.Server.PublicURL,
			}),
			ReadHeaderTimeout: 10 * time.Second,
		}
		mlis, err := net.Listen("tcp", cfg.Server.MCPListen)
		if err != nil {
			g.Stop()
			return fmt.Errorf("mcp_listen: %w", err)
		}
		slog.Info("noted MCP over HTTP and web portal listening", "addr", mlis.Addr().String(), "mcp", mcphttp.Path, "signup", cfg.Auth.Signup)
		go func() {
			if err := hs.Serve(mlis); !errors.Is(err, http.ErrServerClosed) {
				done <- fmt.Errorf("mcp http: %w", err)
			}
		}()
	}

	select {
	case err := <-done:
		g.Stop()
		if hs != nil {
			_ = hs.Close()
		}
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		if hs != nil {
			// Streaming MCP responses can stay open; give them a moment, then cut.
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = hs.Shutdown(sctx)
			cancel()
			_ = hs.Close()
		}
		g.GracefulStop()
		return nil
	}
}

// localConn dials this process's own gRPC listener, which remote MCP sessions
// use as their backend. A wildcard listen address is reached over loopback.
func localConn(addr net.Addr, useTLS bool) (*grpc.ClientConn, error) {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	creds := insecure.NewCredentials()
	if useTLS {
		// Our own certificate is issued for the public name, not loopback.
		creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // loopback to ourselves
	}
	return grpc.NewClient(net.JoinHostPort(host, port), grpc.WithTransportCredentials(creds))
}

func openStore(fs *flag.FlagSet, argv []string) (*store.Store, error) {
	path := configFlag(fs)
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}
	st.SetLocation(cfg.Location())
	return st, nil
}

// splitArgs lets flags appear after the positional arguments:
// `noted user add alice -config x.toml`.
func splitArgs(argv []string) (pos, flags []string) {
	for i := 0; i < len(argv); i++ {
		if len(argv[i]) > 0 && argv[i][0] == '-' {
			flags = append(flags, argv[i])
			if i+1 < len(argv) && len(argv[i]) > 1 && !hasEquals(argv[i]) {
				i++
				flags = append(flags, argv[i])
			}
			continue
		}
		pos = append(pos, argv[i])
	}
	return pos, flags
}

func hasEquals(s string) bool {
	for _, c := range s {
		if c == '=' {
			return true
		}
	}
	return false
}

func userCmd(argv []string) error {
	pos, flags := splitArgs(argv)
	if len(pos) == 0 {
		return fmt.Errorf("usage: noted user add <name> | noted user list | noted user pro <name> [on|off]")
	}
	st, err := openStore(flag.NewFlagSet("user", flag.ExitOnError), flags)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	switch pos[0] {
	case "add":
		if len(pos) != 2 {
			return fmt.Errorf("usage: noted user add <name>")
		}
		u, err := st.EnsureUser(ctx, pos[1])
		if err != nil {
			return err
		}
		tok, err := st.CreateToken(ctx, u.ID, "initial")
		if err != nil {
			return err
		}
		fmt.Printf("user:  %s\ntoken: %s\n\nSend it as gRPC metadata  authorization: Bearer %s\nThe token is shown once; only its hash is stored.\n", u.Name, tok, tok)
	case "list":
		us, err := st.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range us {
			fmt.Printf("%s\t%s\n", u.Name, u.Created.Format("2006-01-02"))
		}
	case "pro":
		if len(pos) < 2 || len(pos) > 3 || (len(pos) == 3 && pos[2] != "on" && pos[2] != "off") {
			return fmt.Errorf("usage: noted user pro <name> [on|off]")
		}
		on := len(pos) == 2 || pos[2] == "on"
		if err := st.SetUserPro(ctx, pos[1], on); err != nil {
			return fmt.Errorf("user %q: %w", pos[1], err)
		}
		fmt.Printf("%s: Pro %s\n", pos[1], map[bool]string{true: "on", false: "off"}[on])
	default:
		return fmt.Errorf("unknown user command %q", pos[0])
	}
	return nil
}

func exportCmd(argv []string) error {
	pos, flags := splitArgs(argv)
	if len(pos) != 1 {
		return fmt.Errorf("usage: noted export <user> [-o file.zip]")
	}
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	out := fs.String("o", "", "output file (default noted-<user>-<date>.zip)")
	st, err := openStore(fs, flags)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Name != pos[0] {
			continue
		}
		now := time.Now()
		path := *out
		if path == "" {
			path = fmt.Sprintf("noted-%s-%s.zip", u.Name, now.Format("20060102"))
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if err := export.Write(ctx, st, u.ID, u.Name, now, f); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	return fmt.Errorf("no user named %q", pos[0])
}

// newCortexSyncer builds the sync of cfg's [cortexdb] section; with connect it
// also opens the connection (which dials lazily, on the first call).
func newCortexSyncer(cfg config.Config, st *store.Store, connect bool) (*cortexsync.Syncer, error) {
	sy := &cortexsync.Syncer{
		Store: st, Target: cfg.Cortex.Remote + "/" + cortexsync.DefaultCollection, Users: cfg.Cortex.SyncUsers,
		Location: cfg.Location(), Lang: cfg.Language, Interval: cfg.Cortex.Every(),
	}
	if connect {
		r, err := cortexsync.Dial(cfg.Cortex.Remote, cfg.Cortex.Token(), cfg.Cortex.TLS)
		if err != nil {
			return nil, err
		}
		sy.Brain = r
	}
	return sy, nil
}

// cortexCmd runs the CortexDB sync for one user now and lists what it sent
// (or, with --dry-run, would send) by item key: counts and ids, no content.
func cortexCmd(argv []string) error {
	const use = "usage: noted cortex sync <user> [--dry-run] [--full] [-config file]"
	var opt cortexsync.Options
	var rest []string
	for _, a := range argv {
		switch a {
		case "--dry-run", "-dry-run":
			opt.DryRun = true
		case "--full", "-full":
			opt.Full = true
		default:
			rest = append(rest, a)
		}
	}
	pos, flags := splitArgs(rest)
	if len(pos) != 2 || pos[0] != "sync" {
		return errors.New(use)
	}
	fs := flag.NewFlagSet("cortex", flag.ExitOnError)
	path := configFlag(fs)
	if err := fs.Parse(flags); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	name := pos[1]
	if !opt.DryRun {
		if cfg.Cortex.Remote == "" {
			return errors.New("set [cortexdb] remote (or NOTED_CORTEXDB_REMOTE) first")
		}
		if !slices.Contains(cfg.Cortex.SyncUsers, name) {
			return fmt.Errorf("%q is not in [cortexdb] sync_users; only listed users are sent (--dry-run works for anyone)", name)
		}
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	st.SetLocation(cfg.Location())
	sy, err := newCortexSyncer(cfg, st, !opt.DryRun)
	if err != nil {
		return err
	}
	if r, ok := sy.Brain.(*cortexsync.Remote); ok {
		defer func() { _ = r.Close() }()
	}
	if opt.DryRun && !sy.OptedIn(name) {
		sy.Users = append(sy.Users, name) // reads only; nothing is sent or recorded
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rep, err := sy.SyncUser(ctx, name, opt)
	if err != nil {
		return err
	}
	mode := ""
	if opt.DryRun {
		mode = " (dry run: nothing sent)"
	}
	remote := cfg.Cortex.Remote
	if remote == "" {
		remote = "(no remote set)"
	}
	fmt.Printf("%s -> %s, collection %s%s\n", name, remote, cortexsync.DefaultCollection, mode)
	verb := map[bool][2]string{true: {"to send", "to delete"}, false: {"sent", "deleted"}}[opt.DryRun]
	printKeys(verb[0], rep.Upserts)
	printKeys(verb[1], rep.Deletes)
	fmt.Printf("unchanged: %d\n", rep.Unchanged)
	if len(rep.Failed) > 0 {
		fmt.Printf("failed: %d (tried again on the next run)\n", len(rep.Failed))
		for _, f := range rep.Failed {
			fmt.Printf("  %s %s: %v\n", f.Op, f.Key, f.Err)
		}
	}
	if rep.Stopped {
		return errors.New("stopped: cortexdb is unreachable or refused the token")
	}
	return nil
}

// printKeys lists item keys grouped by kind.
func printKeys(label string, keys []string) {
	fmt.Printf("%s: %d\n", label, len(keys))
	byKind := map[string][]string{}
	var kinds []string
	for _, k := range keys {
		kind, id, _ := strings.Cut(k, ":")
		if _, ok := byKind[kind]; !ok {
			kinds = append(kinds, kind)
		}
		byKind[kind] = append(byKind[kind], id)
	}
	for _, kind := range kinds {
		fmt.Printf("  %s (%d)\n", kind, len(byKind[kind]))
		for _, id := range byKind[kind] {
			fmt.Printf("    %s\n", id)
		}
	}
}

func tokenCmd(argv []string) error {
	pos, flags := splitArgs(argv)
	if len(pos) != 2 || pos[0] != "new" {
		return fmt.Errorf("usage: noted token new <user> [-label text]")
	}
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	label := fs.String("label", "", "a note about where this token is used")
	st, err := openStore(fs, flags)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Name == pos[1] {
			tok, err := st.CreateToken(ctx, u.ID, *label)
			if err != nil {
				return err
			}
			fmt.Println(tok)
			return nil
		}
	}
	return fmt.Errorf("no user named %q (create it with `noted user add`)", pos[1])
}

// health asks a running server's gRPC health service whether it is serving.
// It is a probe of the local server, so with -tls it does not verify the
// certificate (which is usually issued for a public name, not 127.0.0.1); the
// health service exposes nothing, and no credentials are sent.
func health(argv []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:43872", "server address")
	useTLS := fs.Bool("tls", false, "the server has TLS enabled")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	creds := insecure.NewCredentials()
	if *useTLS {
		creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // local liveness probe only
	}
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("server is %s", resp.Status)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// mcpCmd serves MCP over stdio for clients such as Claude Desktop/Code. It is
// a gRPC client of a running noted server, so it needs that server's address
// and a user token. Stdout carries the protocol; logs go to stderr.
func mcpCmd(argv []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	addr := fs.String("addr", envOr("NOTED_ADDR", "127.0.0.1:43872"), "noted server address (env NOTED_ADDR)")
	token := fs.String("token", os.Getenv("NOTED_TOKEN"), "user token (env NOTED_TOKEN); omit if auth is disabled")
	useTLS := fs.Bool("tls", false, "connect to the server with TLS")
	tz := fs.String("tz", envOr("NOTED_TIME_ZONE", "Local"), "zone for times given without an offset")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	if *tz == "Local" {
		*tz = mcpserver.LocalZone() // the server needs a zone name it can store
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return fmt.Errorf("-tz %q: %w", *tz, err)
	}
	creds := insecure.NewCredentials()
	if *useTLS {
		creds = credentials.NewTLS(nil)
	}
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, inv grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			if *token != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+*token)
			}
			return inv(ctx, method, req, reply, cc, opts...)
		}))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	srv, err := mcpserver.New(probe, conn, loc, version)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &mcp.StdioTransport{})
}
