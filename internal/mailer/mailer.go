package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"mcmods-cn-backend/internal/config"
)

type Mailer struct {
	Config config.SMTPConfig
}

func New(cfg config.SMTPConfig) Mailer {
	return Mailer{Config: cfg}
}

func (m Mailer) Enabled() bool {
	return strings.TrimSpace(m.Config.Host) != "" && strings.TrimSpace(m.Config.From) != ""
}

func (m Mailer) Send(to string, subject string, body string) error {
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
	addr := fmt.Sprintf("%s:%d", m.Config.Host, m.Config.Port)
	headers := []string{
		fmt.Sprintf("From: %s", fromAddress.String()),
		fmt.Sprintf("To: %s", toAddress.String()),
		fmt.Sprintf("Subject: %s", mime.QEncoding.Encode("UTF-8", subject)),
		"MIME-Version: 1.0",
		`Content-Type: text/plain; charset="UTF-8"`,
	}
	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body

	var auth smtp.Auth
	if m.Config.Username != "" {
		auth = smtp.PlainAuth("", m.Config.Username, m.Config.Password, m.Config.Host)
	}
	return sendSMTP(addr, m.Config.Host, auth, fromAddress.Address, []string{toAddress.Address}, []byte(message), m.Config.UseTLS)
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

func sendSMTP(addr string, host string, auth smtp.Auth, from string, to []string, msg []byte, requireTLS bool) error {
	connection, err := (&net.Dialer{Timeout: 15 * time.Second}).Dial("tcp", addr)
	if err != nil {
		return err
	}
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
		return err
	}
	return client.Quit()
}
