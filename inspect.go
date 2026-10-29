package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stepaninn/soho-adapter/internal/soho"
)

type inspectOptions struct {
	ChatID       string
	ChatNumber   int
	MessageLimit int
	Out          string
	Hosts        string
	MaxBytes     int64
}

func parseOptions(args []string, out io.Writer) (inspectOptions, error) {
	var options inspectOptions
	flags := flag.NewFlagSet("soho-adapter", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&options.ChatID, "chat-id", "", "GraphQL ID выбранного чата")
	flags.IntVar(&options.ChatNumber, "chat-number", 0, "Номер чата на первой странице, начиная с 1")
	flags.IntVar(&options.MessageLimit, "message-limit", 20, "Число запрашиваемых сообщений")
	flags.StringVar(&options.Out, "out", "", "Приватный каталог материалов")
	flags.StringVar(&options.Hosts, "allow-file-host", "", "Разрешённые хосты файлов через запятую, включает скачивание")
	flags.Int64Var(&options.MaxBytes, "max-file-bytes", 20<<20, "Предел размера одного файла")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 || options.ChatNumber < 0 || options.MessageLimit <= 0 || options.MaxBytes <= 0 || options.MaxBytes > 1<<30 {
		return options, fmt.Errorf("invalid command options")
	}
	if options.ChatID != "" && options.ChatNumber != 0 {
		return options, fmt.Errorf("choose chat-id or chat-number")
	}
	if (options.Out != "" || options.Hosts != "") && options.ChatID == "" && options.ChatNumber == 0 {
		return options, fmt.Errorf("choose a chat before exporting materials")
	}
	if options.Hosts != "" && options.Out == "" {
		return options, fmt.Errorf("downloads require --out")
	}
	if options.Hosts != "" {
		downloader, err := soho.NewDownloader(soho.DownloadOptions{AllowedHosts: strings.Split(options.Hosts, ","), MaxBytes: options.MaxBytes, Timeout: 30 * time.Second})
		if err != nil {
			return options, err
		}
		downloader.Close()
	}
	if options.Out != "" {
		if err := soho.PrivateDirectory(options.Out); err != nil {
			return options, err
		}
	}
	return options, nil
}

func inspectHomework(ctx context.Context, out io.Writer, client *soho.Client, options inspectOptions) error {
	bundle, err := client.GetHomeworkBundle(ctx, options.ChatID, options.MessageLimit)
	if err != nil {
		return err
	}
	materials := soho.NormalizeHomework(bundle)
	fmt.Fprintf(out, "Chat: %s\nMessages: %d\nHistory complete: %t\n", materials.ChatURL, len(bundle.Messages), bundle.Complete)
	for _, warning := range materials.Warnings {
		fmt.Fprintln(out, "Warning:", warning)
	}
	hosts := make(map[string]bool)
	attachments := []soho.Attachment{}
	for _, message := range materials.Messages {
		attachments = append(attachments, message.Attachments...)
		fmt.Fprintf(out, "Message: %d links, %d attachments\n", len(message.Links), len(message.Attachments))
		for _, attachment := range message.Attachments {
			u, err := url.Parse(attachment.URL)
			if err == nil && u.Scheme == "https" && u.User == nil && u.Hostname() != "" {
				hosts[u.Hostname()] = true
			}
		}
	}
	for host := range hosts {
		fmt.Fprintln(out, "Attachment host (review before allowing):", host)
	}
	if options.Out == "" {
		return nil
	}
	manifest := struct {
		Materials soho.HomeworkMaterials
		Downloads []soho.DownloadResult
	}{Materials: materials, Downloads: []soho.DownloadResult{}}
	failed := false
	var downloader *soho.Downloader
	if options.Hosts != "" {
		downloader, err = soho.NewDownloader(soho.DownloadOptions{AllowedHosts: strings.Split(options.Hosts, ","), MaxBytes: options.MaxBytes, Timeout: 30 * time.Second})
		if err != nil {
			return err
		}
		defer downloader.Close()
	}
	for _, attachment := range attachments {
		result := soho.DownloadResult{Attachment: attachment, State: "not-requested"}
		if downloader != nil {
			result, err = downloader.DownloadAttachment(ctx, attachment, options.Out)
			if err != nil {
				failed = true
			}
		}
		manifest.Downloads = append(manifest.Downloads, result)
		fmt.Fprintln(out, "Attachment:", result.State)
	}
	root, err := os.OpenRoot(options.Out)
	if err != nil {
		return fmt.Errorf("cannot open manifest directory")
	}
	defer root.Close()
	name := "manifest-" + rand.Text() + ".json"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot create manifest")
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	writeErr := encoder.Encode(manifest)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		root.Remove(name)
		return fmt.Errorf("cannot write manifest")
	}
	fmt.Fprintln(out, "Manifest:", filepath.Join(options.Out, name))
	if failed {
		return fmt.Errorf("some attachments were not downloaded; see manifest")
	}
	return nil
}
