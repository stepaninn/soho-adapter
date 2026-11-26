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
	MetadataOnly bool
	ChatID       string
	ChatNumber   int
	MessageLimit int
	Out          string
	Hosts        string
	MaxBytes     int64
	Verbose      bool
	MCP          bool
	FetchLinks   bool
	EnvFile      string
	RolesFile    string
	AuthorRoles  map[string]map[string]string
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
	flags.Int64Var(&options.MaxBytes, "max-file-bytes", 20<<20, "Предел размера одного файла (в режиме MCP по умолчанию 100 МиБ)")
	flags.StringVar(&options.EnvFile, "env-file", ".env", "Путь к локальным настройкам")
	flags.BoolVar(&options.MetadataOnly, "metadata-only", false, "Сохранить описание материалов без скачивания")
	flags.BoolVar(&options.Verbose, "verbose", false, "Подробная диагностика")
	flags.BoolVar(&options.MCP, "mcp", false, "Запустить MCP по stdin/stdout")
	flags.BoolVar(&options.FetchLinks, "fetch-links", false, "Получить содержимое внешних ссылок")
	flags.StringVar(&options.RolesFile, "roles-file", "", "JSON сопоставления профилей авторов по чатам")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	values, err := readEnvFile(options.EnvFile)
	if err != nil {
		return options, err
	}
	if options.RolesFile == "" {
		options.RolesFile = setting(values, "SOHO_AUTHOR_ROLES_FILE")
	}
	if options.MCP || options.Out != "" {
		extraHosts := options.Hosts
		if extraHosts == "" {
			extraHosts = setting(values, "SOHO_RESOURCE_HOSTS")
		}
		options.Hosts = strings.Join(soho.DefaultResourceHosts(), ",")
		if extraHosts != "" {
			options.Hosts += "," + extraHosts
		}
	}
	if options.MetadataOnly {
		if options.MCP || options.FetchLinks {
			return options, fmt.Errorf("metadata-only cannot be combined with MCP or fetch-links")
		}
		options.Hosts = ""
	}
	if options.MCP {
		customMaxBytes := false
		flags.Visit(func(f *flag.Flag) { customMaxBytes = customMaxBytes || f.Name == "max-file-bytes" })
		if !customMaxBytes {
			options.MaxBytes = 100 << 20
		}
		if options.ChatID != "" || options.ChatNumber != 0 || options.FetchLinks {
			return options, fmt.Errorf("MCP selects homework through tools")
		}
		if options.Out == "" {
			options.Out = setting(values, "SOHO_OUTPUT_DIR")
			if options.Out == "" {
				options.Out = "downloads/mcp"
			}
			if !filepath.IsAbs(options.Out) {
				options.Out = filepath.Join(filepath.Dir(options.EnvFile), options.Out)
			}
		}
	}
	if options.RolesFile != "" && !filepath.IsAbs(options.RolesFile) {
		options.RolesFile = filepath.Join(filepath.Dir(options.EnvFile), options.RolesFile)
	}
	options.AuthorRoles, err = readAuthorRoles(options.RolesFile)
	if err != nil {
		return options, err
	}
	if flags.NArg() != 0 || options.ChatNumber < 0 || options.MessageLimit <= 0 || options.MaxBytes <= 0 || options.MaxBytes > 1<<30 {
		return options, fmt.Errorf("invalid command options")
	}
	if options.ChatID != "" && options.ChatNumber != 0 {
		return options, fmt.Errorf("choose chat-id or chat-number")
	}
	if (options.Out != "" || options.Hosts != "") && options.ChatID == "" && options.ChatNumber == 0 && !options.MCP {
		return options, fmt.Errorf("choose a chat before exporting materials")
	}
	if (options.Hosts != "" || options.FetchLinks) && options.Out == "" {
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
	materials := soho.NormalizeHomeworkWithRoles(bundle, options.AuthorRoles[bundle.ChatID])
	fmt.Fprintf(out, "Chat: %s\nMessages: %d\nHistory complete: %t\n", materials.ChatURL, len(bundle.Messages), bundle.Complete)
	var student *string
	if bundle.ChatPeer != nil && bundle.ChatPeer.TypeName == "ChatPeerHomework" && bundle.ChatPeer.Student != nil {
		student = bundle.ChatPeer.Student.Name
	}
	fmt.Fprintln(out, "Ученик:", displayValue(student))
	fmt.Fprintln(out, "Заголовок чата:", displayValue(bundle.Title))
	fmt.Fprintln(out, "Последнее обновление:", displayValue(bundle.LastUpdated))
	if bundle.StudentHomework != nil {
		fmt.Fprintln(out, "Срок сдачи:", displayValue(bundle.StudentHomework.DeadlineAt))
	}
	seenWarnings := make(map[string]bool)
	for _, warning := range materials.Warnings {
		if !seenWarnings[warning] && options.Verbose {
			fmt.Fprintln(out, "Warning:", warning)
			seenWarnings[warning] = true
		}
	}
	hosts := make(map[string]bool)
	attachments := []soho.Attachment{}
	for index, message := range materials.Messages {
		attachments = append(attachments, message.Attachments...)
		if options.Verbose {
			fmt.Fprintf(out, "Message: %d links, %d attachments\n", len(message.Links), len(message.Attachments))
			created := bundle.Messages[index].TimeCreated
			if created == "" {
				created = "не указана"
			}
			fmt.Fprintln(out, "Дата сообщения:", created)
		}
		for _, attachment := range message.Attachments {
			u, err := url.Parse(attachment.URL)
			if err == nil && u.Scheme == "https" && u.User == nil && u.Hostname() != "" {
				hosts[u.Hostname()] = true
			}
		}
	}
	if options.Verbose {
		for host := range hosts {
			fmt.Fprintln(out, "Attachment host (review before allowing):", host)
		}
	}
	printMaterialGroups(out, materials)
	if options.Out == "" {
		return nil
	}
	if options.Hosts == "" && len(attachments) > 0 {
		fmt.Fprintln(out, "Скачивание не запрошено: сохранены только метаданные")
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
			result, err = downloader.DownloadAttachment(ctx, attachment, roleDirectory(options.Out, attachment.Role))
			if err != nil {
				failed = true
			}
		}
		manifest.Downloads = append(manifest.Downloads, result)
		if options.Verbose {
			fmt.Fprintln(out, "Attachment:", result.State)
		}
	}
	for _, message := range materials.Messages {
		for _, link := range message.Links {
			result := soho.DownloadResult{Attachment: soho.Attachment{MaterialReference: link}, State: "not-requested"}
			if options.FetchLinks && downloader != nil {
				result, err = downloader.FetchLink(ctx, link, roleDirectory(options.Out, link.Role))
				if err != nil {
					failed = true
				}
			}
			manifest.Downloads = append(manifest.Downloads, result)
		}
	}
	downloaded, pending := 0, 0
	for _, result := range manifest.Downloads {
		if result.State == "downloaded" {
			downloaded++
		} else {
			pending++
		}
	}
	fmt.Fprintf(out, "Получено материалов: %d; не получено: %d\n", downloaded, pending)
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
		return fmt.Errorf("some materials were not fetched; see manifest")
	}
	return nil
}

func displayValue(value *string) string {
	if value == nil || *value == "" {
		return "не указано"
	}
	return *value
}

func roleDirectory(base, role string) string {
	if role != "teacher" && role != "student" {
		role = "unknown"
	}
	return filepath.Join(base, role)
}

func printMaterialGroups(out io.Writer, materials soho.HomeworkMaterials) {
	for _, role := range []string{"teacher", "student", "unknown"} {
		count, files, links := 0, 0, 0
		for _, message := range materials.Messages {
			if message.Role == role {
				count++
				files += len(message.Attachments)
				links += len(message.Links)
			}
		}
		if count > 0 {
			fmt.Fprintf(out, "%s: сообщений %d, файлов %d, ссылок %d\n", map[string]string{"teacher": "Преподаватель", "student": "Ученик", "unknown": "Неопределённая роль"}[role], count, files, links)
		}
	}
	if !materials.Bundle.Complete {
		fmt.Fprintln(out, "Внимание: контекст или история неполны")
	}
	for _, message := range materials.Messages {
		if message.Role == "unknown" {
			fmt.Fprintln(out, "Не все авторы найдены среди участников чата; имена и ID сохранены в manifest")
			break
		}
	}
}
