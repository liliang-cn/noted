// Command noted is a self-hosted notes and calendar gRPC service with
// optional AI features.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // zones must work in scratch/distroless images

	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/config"
	"github.com/liliang-cn/noted/internal/export"
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
  noted token new <user> [-label text]      issue another token for an existing user
  noted export <user> [-o file.zip]         write everything a user has (JSON, Markdown notes, .ics) to a zip
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

	done := make(chan error, 1)
	go func() { done <- g.Serve(lis) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		g.GracefulStop()
		return nil
	}
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
	return store.Open(cfg.DBPath())
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
		return fmt.Errorf("usage: noted user add <name> | noted user list")
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
