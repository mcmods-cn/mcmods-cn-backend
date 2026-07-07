package mailer

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

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
	addr := fmt.Sprintf("%s:%d", m.Config.Host, m.Config.Port)
	headers := []string{
		fmt.Sprintf("From: %s", m.Config.From),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		`Content-Type: text/plain; charset="UTF-8"`,
	}
	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body

	var auth smtp.Auth
	if m.Config.Username != "" {
		auth = smtp.PlainAuth("", m.Config.Username, m.Config.Password, m.Config.Host)
	}
	if m.Config.UseTLS {
		return sendWithStartTLS(addr, m.Config.Host, auth, m.Config.From, []string{to}, []byte(message))
	}
	return smtp.SendMail(addr, auth, m.Config.From, []string{to}, []byte(message))
}

func sendWithStartTLS(addr string, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
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

func LocalAddress(raddr string) string {
	host, _, err := net.SplitHostPort(raddr)
	if err != nil {
		return raddr
	}
	return host
}
