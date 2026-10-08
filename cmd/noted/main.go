// Command noted is a self-hosted notes and calendar gRPC service with
// optional AI features.
package main

import (
	"context"
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
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/server"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var version = "dev"

const usage = `noted - self-hosted notes and calendar over gRPC

Usage:
  noted serve [-config noted.toml]          run the server
  noted user add <name> [-config ...]       create a user and print its first token
  noted user list [-config ...]
  noted token new <user> [-label text]      issue another token for an existing user
  noted health [-addr host:port]            exit 0 if a server answers (for container healthchecks)
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
	fs.Parse(argv)

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
	defer st.Close()

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
		Store: st, Auth: authn, Hub: reminder.NewHub(), Location: cfg.Location(), Reflection: cfg.Server.Reflection,
	}

	if cfg.AI.Enabled {
		eng, err := ai.New(ctx, ai.Options{
			Store: st, LLM: cfg.AI.LLM, Embed: cfg.AI.Embedding, Dir: cfg.AIDir(), Location: cfg.Location(),
		})
		if err != nil {
			return fmt.Errorf("start AI: %w", err)
		}
		defer eng.Close()
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
	defer st.Close()
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
	defer st.Close()
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
// It talks plaintext, so it is meant for the container-local probe; with TLS
// enabled, point a real probe at the port instead.
func health(argv []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:43872", "server address")
	fs.Parse(argv)
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
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
