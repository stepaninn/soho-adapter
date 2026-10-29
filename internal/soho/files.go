package soho

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DownloadOptions struct {
	AllowedHosts []string
	MaxBytes     int64
	Timeout      time.Duration
}

type Downloader struct {
	client   *http.Client
	hosts    map[string]bool
	maxBytes int64
}

type DownloadResult struct {
	Attachment Attachment
	Path       string
	Bytes      int64
	MIMEType   string
	State      string
	Error      string
}

func NewDownloader(options DownloadOptions) (*Downloader, error) {
	if options.MaxBytes <= 0 || options.MaxBytes > 1<<30 || options.Timeout <= 0 || len(options.AllowedHosts) == 0 {
		return nil, fmt.Errorf("download requires trusted hosts, a positive timeout and size limit up to 1 GiB")
	}
	hosts := make(map[string]bool)
	for _, host := range options.AllowedHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || strings.ContainsAny(host, "/:@*?#") || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil {
			return nil, fmt.Errorf("invalid trusted download host")
		}
		hosts[host] = true
	}
	transport := &http.Transport{
		// Разрешаем только публичные адреса и подключаемся к проверенному IP
		DialContext:           dialPublic,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:    true,
	}
	return &Downloader{hosts: hosts, maxBytes: options.MaxBytes, client: &http.Client{Transport: transport, Timeout: options.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (d *Downloader) Close() { d.client.CloseIdleConnections() }

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "192.88.99.0/24", "3fff::/20", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/23", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(block).Contains(address) {
			return false
		}
	}
	return true
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid download address")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("cannot resolve download host")
	}
	for _, ip := range addresses {
		if !publicAddress(ip) {
			return nil, fmt.Errorf("download host resolves to a restricted address")
		}
	}
	var dialer net.Dialer
	for _, ip := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, fmt.Errorf("cannot connect to download host")
}

func (d *Downloader) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") || !d.hosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("attachment URL is not an allowed HTTPS URL")
	}
	return nil
}

func PrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("cannot create private output directory")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("output directory must be private (0700)")
	}
	return nil
}

func (d *Downloader) DownloadAttachment(ctx context.Context, attachment Attachment, directory string) (DownloadResult, error) {
	result := DownloadResult{Attachment: attachment, State: "failed"}
	fail := func(message string) (DownloadResult, error) {
		result.Error = message
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("download canceled: %w", err)
		}
		return result, errors.New(message)
	}
	if err := ctx.Err(); err != nil {
		return fail("download canceled")
	}
	if attachment.Purpose != "file" && attachment.Purpose != "inline-image" {
		return fail("resource is not a downloadable attachment")
	}
	if err := d.validateURL(attachment.URL); err != nil {
		return fail(err.Error())
	}
	if attachment.Name == "." || attachment.Name == ".." || strings.ContainsAny(attachment.Name, "/\\\x00\r\n") || filepath.IsAbs(attachment.Name) {
		return fail("unsafe attachment name")
	}
	if attachment.Size != nil && (*attachment.Size < 0 || *attachment.Size > d.maxBytes) {
		return fail("attachment metadata exceeds size limit")
	}
	if attachment.MIMEType != "" && !supportedMIME(attachment.MIMEType) {
		result.State = "unsupported"
		return fail("unsupported attachment MIME type")
	}
	if err := PrivateDirectory(directory); err != nil {
		return fail(err.Error())
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fail("cannot open output directory")
	}
	defer root.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, attachment.URL, nil)
	if err != nil {
		return fail("cannot create download request")
	}
	// Отдельный клиент не содержит токен, cookie и контекст организации
	response, err := d.client.Do(req)
	if err != nil {
		return fail("attachment request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail(fmt.Sprintf("attachment returned HTTP %d", response.StatusCode))
	}
	if response.ContentLength > d.maxBytes {
		return fail("attachment exceeds size limit")
	}
	temporary := ".part-" + rand.Text()
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fail("cannot create temporary attachment")
	}
	defer root.Remove(temporary)
	defer file.Close()
	count, err := io.Copy(file, io.LimitReader(response.Body, d.maxBytes+1))
	if err != nil {
		return fail("cannot read attachment body")
	}
	if count > d.maxBytes {
		return fail("attachment exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return fail("download canceled")
	}
	if attachment.Size != nil && count != *attachment.Size {
		return fail("attachment size differs from metadata")
	}
	if response.ContentLength >= 0 && count != response.ContentLength {
		return fail("attachment size differs from Content-Length")
	}
	if err := file.Close(); err != nil {
		return fail("cannot finish attachment write")
	}
	saved, err := root.Open(temporary)
	if err != nil {
		return fail("cannot inspect attachment")
	}
	header := make([]byte, 512)
	n, readErr := saved.Read(header)
	saved.Close()
	if readErr != nil && readErr != io.EOF {
		return fail("cannot inspect attachment signature")
	}
	actual := http.DetectContentType(header[:n])
	if bytes.HasPrefix(header[:n], []byte("%PDF-")) {
		actual = "application/pdf"
	}
	if !supportedMIME(actual) {
		result.State = "unsupported"
		return fail("unsupported attachment signature")
	}
	if attachment.Purpose == "inline-image" && actual == "application/pdf" {
		return fail("inline image is not an image")
	}
	declared := response.Header.Get("Content-Type")
	if declared != "" {
		mediaType, _, err := mime.ParseMediaType(declared)
		if err != nil || (mediaType != "application/octet-stream" && mediaType != actual) {
			return fail("attachment Content-Type differs from signature")
		}
	}
	if attachment.MIMEType != "" && attachment.MIMEType != actual {
		return fail("attachment metadata MIME differs from signature")
	}
	extension := map[string]string{"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg"}[actual]
	name := "attachment-" + rand.Text() + extension
	// Публикуем файл атомарно без перезаписи существующего пути
	if err := root.Link(temporary, name); err != nil {
		return fail("cannot publish attachment")
	}
	result.Path, result.Bytes, result.MIMEType, result.State = filepath.Join(directory, name), count, actual, "downloaded"
	return result, nil
}

func supportedMIME(value string) bool {
	return value == "application/pdf" || value == "image/png" || value == "image/jpeg"
}
