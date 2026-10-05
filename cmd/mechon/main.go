// Command mechon is the panel: the API, the embedded web UI and background jobs.
//
//	mechon serve   run the panel (the default)
//	mechon init    create the first admin account
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/config"
	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/panel"
	"github.com/jub0t/mechon/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx)
	case "init":
		err = initAdmin(ctx, args)
	default:
		err = fmt.Errorf("unknown command %q (want serve or init)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mechon:", err)
		os.Exit(1)
	}
}

func connect(ctx context.Context) (config.Panel, *pgxpool.Pool, error) {
	cfg, err := config.LoadPanel()
	if err != nil {
		return cfg, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return cfg, nil, fmt.Errorf("database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return cfg, nil, fmt.Errorf("database: %w", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return cfg, nil, err
	}
	return cfg, pool, nil
}

func serve(ctx context.Context) error {
	cfg, pool, err := connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := panel.New(cfg, pool, web.Dist())
	go srv.Background(ctx)

	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	slog.Info("panel listening", "addr", cfg.Listen, "public_url", cfg.PublicURL.String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return hs.Shutdown(shutdownCtx)
}

func initAdmin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	email := fs.String("email", "", "admin email (required)")
	name := fs.String("name", "Admin", "display name")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from stdin instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	*email = strings.ToLower(strings.TrimSpace(*email))
	if *email == "" || !strings.Contains(*email, "@") {
		return errors.New("init: --email is required")
	}

	_, pool, err := connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := db.New(pool)
	if n, err := q.CountAdmins(ctx); err != nil {
		return err
	} else if n > 0 {
		return errors.New("init: an admin already exists; sign in to the panel to add more")
	}

	password, err := readPassword(*passwordStdin)
	if err != nil {
		return err
	}
	if len(password) < auth.MinPasswordLen || len(password) > auth.MaxPasswordLen {
		return fmt.Errorf("init: password must be %d to %d characters", auth.MinPasswordLen, auth.MaxPasswordLen)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: *email, Name: strings.TrimSpace(*name), PasswordHash: &hash, Role: db.UserRoleAdmin})
	if err != nil {
		return err
	}
	fmt.Printf("Created admin %s. Sign in at your MECHON_PUBLIC_URL.\n", u.Email)
	return nil
}

func readPassword(fromStdin bool) (string, error) {
	if fromStdin || !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("init: reading password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "Password: ")
	p1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Repeat password: ")
	p2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("init: passwords do not match")
	}
	return string(p1), nil
}
