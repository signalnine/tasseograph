// internal/collector/mail.go
package collector

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"

	"github.com/signalnine/tasseograph/internal/config"
)

// SendMail dispatches one plain-text email via SMTP. STARTTLS + PLAIN auth
// are both opt-in via config, so the same code path works against an
// authenticated submission service (Fastmail :587) or a local relay (:25).
//
// Wire format: a single plain-text part with CRLF line endings. We don't
// bother with multipart/HTML -- the digest body is intentionally terminal-
// friendly and the recipient (on-call) reads it as text.
func SendMail(cfg *config.CollectorConfig, subject, body string) error {
	if cfg.SMTPHost == "" || cfg.SMTPFrom == "" || cfg.SMTPTo == "" {
		return errors.New("SMTPHost, SMTPFrom, SMTPTo all required")
	}

	host, _, err := net.SplitHostPort(cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp_host must be host:port: %w", err)
	}

	conn, err := net.Dial("tcp", cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cfg.SMTPHost, err)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close()

	if err := client.Hello(localHelo()); err != nil {
		return fmt.Errorf("EHLO: %w", err)
	}

	// STARTTLS upgrade. Required for Fastmail submission; harmless on a
	// local relay if disabled.
	if cfg.SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("server does not advertise STARTTLS but smtp_starttls is set")
		}
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}

	// Auth, if a password was wired in. PLAIN is universally supported and
	// safe over the TLS we just negotiated. Submitting plain-PLAIN against
	// a non-TLS server would leak credentials, so refuse that combination.
	if cfg.SMTPPassword != "" {
		if !cfg.SMTPStartTLS {
			return errors.New("refusing to send SMTP password without STARTTLS")
		}
		auth := smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("AUTH: %w", err)
		}
	}

	if err := client.Mail(cfg.SMTPFrom); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(cfg.SMTPTo); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := wc.Write(buildMessage(cfg.SMTPFrom, cfg.SMTPTo, subject, body)); err != nil {
		wc.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close DATA: %w", err)
	}

	return client.Quit()
}

// buildMessage assembles RFC 5322 headers + a plain-text body with CRLF
// line endings. The net/smtp package's Data() writer already dot-stuffs and
// terminates with the EOM marker, so we just produce a well-formed RFC822
// message and let the library handle the SMTP-specific transformations.
func buildMessage(from, to, subject, body string) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", from)
	fmt.Fprintf(&sb, "To: %s\r\n", to)
	fmt.Fprintf(&sb, "Subject: %s\r\n", subject)
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("\r\n")
	body = strings.ReplaceAll(body, "\r\n", "\n")
	for _, line := range strings.Split(body, "\n") {
		sb.WriteString(line)
		sb.WriteString("\r\n")
	}
	return []byte(sb.String())
}

// localHelo is the name we announce in EHLO. Fastmail and most providers
// don't care what it is so long as it's syntactically valid; we use the OS
// hostname when available, else a placeholder.
func localHelo() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "tasseograph.local"
}
