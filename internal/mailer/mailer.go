package mailer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"mcmods-cn-backend/internal/config"
)

// ErrAcceptanceUnknown means the DATA terminator may have reached the peer
// but its final reply was lost. The SMTP transaction cannot be retried safely.
var ErrAcceptanceUnknown = errors.New("SMTP acceptance is unknown")

type Mailer struct {
	Config config.SMTPConfig
}

func New(cfg config.SMTPConfig) Mailer {
	return Mailer{Config: cfg}
}

func (m Mailer) Enabled() bool {
	return m.Config.Enabled && m.smtpHost() != "" && strings.TrimSpace(m.Config.From) != "" && m.Config.Port > 0 && m.Config.Port <= 65535
}

func (m Mailer) smtpHost() string {
	host := strings.TrimSpace(m.Config.Host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// SameAuthenticationTarget reports whether both configurations send SMTP
// authentication to the same endpoint as the same login.
func (m Mailer) SameAuthenticationTarget(other Mailer) bool {
	if m.Config.Port != other.Config.Port || m.Config.Username != other.Config.Username {
		return false
	}
	a, b := m.smtpHost(), other.smtpHost()
	if a == b {
		return true
	}
	// DNS names are ASCII case-insensitive. Keep absolute names and IPv6
	// zone identifiers distinct from their possible local counterparts.
	return !strings.ContainsAny(a+b, ":%") && strings.EqualFold(a, b) && strings.IndexFunc(a+b, func(r rune) bool { return r > 127 }) < 0
}

func (m Mailer) Send(to string, subject string, body string) error {
	return m.SendEvent(context.Background(), to, subject, body, "")
}

func (m Mailer) SendEvent(ctx context.Context, to, subject, body, eventID string) error {
	if !m.Enabled() {
		return fmt.Errorf("mail system is not configured")
	}
	fromAddress, err := parseMailbox(m.Config.From)
	if err != nil {
		return fmt.Errorf("invalid sender address: %w", err)
	}
	toAddress, err := parseMailbox(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.New("mail subject contains a line break")
	}
	host := m.smtpHost()
	addr := net.JoinHostPort(host, strconv.Itoa(m.Config.Port))
	headers := []string{
		fmt.Sprintf("From: %s", fromAddress.String()),
		fmt.Sprintf("To: %s", toAddress.String()),
		fmt.Sprintf("Subject: %s", mime.QEncoding.Encode("UTF-8", subject)),
		"MIME-Version: 1.0",
		`Content-Type: text/plain; charset="UTF-8"`,
	}
	if eventID != "" {
		domain := fromAddress.Address[strings.LastIndexByte(fromAddress.Address, '@')+1:]
		headers = append(headers, fmt.Sprintf("Message-ID: <mcmods.%x@%s>", sha256.Sum256([]byte(eventID)), domain))
	}
	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body

	var auth smtp.Auth
	if m.Config.Username != "" {
		auth = smtp.PlainAuth("", m.Config.Username, m.Config.Password, host)
	}
	return sendSMTP(ctx, addr, host, auth, fromAddress.Address, []string{toAddress.Address}, []byte(message), m.Config.UseTLS)
}

func parseMailbox(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("mailbox contains a line break")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(address.Address) == "" {
		return nil, errors.New("mailbox is malformed")
	}
	return address, nil
}

func sendSMTP(ctx context.Context, addr string, host string, auth smtp.Auth, from string, to []string, msg []byte, requireTLS bool) error {
	connection, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	if err = connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		_ = connection.Close()
		return err
	}
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		_ = connection.Close()
		return err
	}
	defer client.Close()

	if requireTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return errors.New("SMTP server does not support required STARTTLS")
		}
		tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if auth != nil {
		ok, _ := client.Extension("AUTH")
		if !ok {
			return errors.New("SMTP server does not support required authentication")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		var reply *textproto.Error
		if errors.As(err, &reply) && reply.Code >= 400 && reply.Code < 600 {
			return err
		}
		return errors.Join(ErrAcceptanceUnknown, err)
	}
	// A successful final DATA reply transfers responsibility to the peer.
	// Failure to close that session with QUIT cannot undo acceptance.
	_ = client.Quit()
	return nil
}
