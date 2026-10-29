package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/stepaninn/soho-adapter/internal/soho"
)

func main() {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err == nil {
		err = runConfigured(os.Stdout, options)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	return runConfigured(out, inspectOptions{MessageLimit: 20, MaxBytes: 20 << 20})
}

func runConfigured(out io.Writer, options inspectOptions) error {
	login, password, err := credentialsFromEnvFile(".env")
	if err != nil {
		return err
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

	fmt.Fprintln(out, "Logging in...")

	if err := client.Authenticate(ctx, login, password); err != nil {
		return fmt.Errorf("prepare session: %w", err)
	}

	fmt.Fprintln(out, "Verifying access to organization...")

	if _, err := client.CurrentOrganization(ctx); err != nil {
		return fmt.Errorf("read organization: %w", err)
	}

	fmt.Fprintln(out, "The login has been completed, organization context has been confirmed")

	if options.ChatID != "" {
		return inspectHomework(ctx, out, client, options)
	}

	page, err := client.ListHomeworkChatsPage(ctx, soho.ListHomeworkChatsOptions{
		OnlyAttention: true,
		OnlyMy:        true,
		SortDesc:      false,
		Limit:         30,
	})
	if err != nil {
		return fmt.Errorf("read homework chat page: %w", err)
	}

	fmt.Fprintf(out, "Homework chats on this page: %d\n", len(page.Chats))
	for i, chat := range page.Chats {
		status := "unavailable (no studentHomework)"
		if chat.StudentHomework != nil {
			status = chat.StudentHomework.MergedStatus
			if status == "" {
				status = "unavailable (empty mergedStatus)"
			}
		}
		fmt.Fprintf(out, "%d. Status: %s\n", i+1, status)
		fmt.Fprintln(out, "Chat:", soho.ChatURL(chat.UID))
	}
	if page.PageInfo.HasNextPage {
		fmt.Fprintln(out, "More chats are available; only the first page was loaded.")
	}

	if options.ChatNumber > 0 {
		if options.ChatNumber > len(page.Chats) {
			return fmt.Errorf("chat number exceeds page size")
		}
		options.ChatID = page.Chats[options.ChatNumber-1].ID
		return inspectHomework(ctx, out, client, options)
	}
	return nil
}
