package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/stepaninn/soho-adapter/internal/soho"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run() error {
	login := os.Getenv("SOHO_LOGIN")
	password := os.Getenv("SOHO_PASSWORD")

	if login == "" || password == "" {
		return fmt.Errorf("set SOHO_LOGIN and SOHO_PASSWORD")
	}

	signalCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()

	ctx, cancel := context.WithTimeout(
		signalCtx,
		90*time.Second,
	)
	defer cancel()

	client, err := soho.NewClient("https://api.soholms.com")
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	fmt.Println("Logging in...")

	if err := client.Authenticate(ctx, login, password); err != nil {
		return fmt.Errorf("prepare session: %w", err)
	}

	fmt.Println("Verifying access to organization...")

	if _, err := client.CurrentOrganization(ctx); err != nil {
		return fmt.Errorf("read organization: %w", err)
	}

	fmt.Println("The login has been completed, organization context has been confirmed")

	return nil
}
