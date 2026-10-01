package mailer

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

type test024SMTPBehavior struct {
	tlsConfig *tls.Config
	auth      bool
	reject    string
	stall     bool
}

type test024SMTPResult struct {
	commands []string
	message  []byte
	tls      uint16
	auth     bool
	err      error
}

// A real, owned loopback SMTP peer. It never contacts an external mailbox.
func newTEST024SMTP(t *testing.T, behavior test024SMTPBehavior) (int, <-chan test024SMTPResult) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan test024SMTPResult, 1)
	done := make(chan struct{})
	var lock sync.Mutex
	var active net.Conn
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		lock.Lock()
		active = connection
		lock.Unlock()
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(40 * time.Second))
		report := test024SMTPResult{}
		defer func() { result <- report }()
		if behavior.stall {
			var byte [1]byte
			_, report.err = connection.Read(byte[:])
			return
		}
		reader := bufio.NewReader(connection)
		write := func(value string) bool {
			_, report.err = io.WriteString(connection, value+"\r\n")
			return report.err == nil
		}
		if !write("220 localhost owned TEST024 SMTP") {
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				report.err = err
				return
			}
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			verb, value, _ := strings.Cut(line, " ")
			// Do not retain a credential-bearing AUTH command in diagnostics.
			if verb == "AUTH" {
				report.commands = append(report.commands, "AUTH")
			} else {
				report.commands = append(report.commands, line)
			}
			switch verb {
			case "EHLO":
				if !write("250-localhost") {
					return
				}
				if behavior.tlsConfig != nil && report.tls == 0 {
					if !write("250-STARTTLS") {
						return
					}
				}
				if behavior.auth {
					if !write("250-AUTH PLAIN") {
						return
					}
				}
				if !write("250 OK") {
					return
				}
			case "STARTTLS":
				if behavior.tlsConfig == nil {
					report.err = errors.New("unexpected STARTTLS")
					return
				}
				if !write("220 upgrade") {
					return
				}
				secured := tls.Server(connection, behavior.tlsConfig)
				if report.err = secured.Handshake(); report.err != nil {
					return
				}
				report.tls = secured.ConnectionState().Version
				connection = secured
				reader = bufio.NewReader(connection)
			case "AUTH":
				if !behavior.auth {
					report.err = errors.New("AUTH without capability")
					return
				}
				mechanism, encoded, _ := strings.Cut(value, " ")
				decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
				if mechanism != "PLAIN" || decodeErr != nil || string(decoded) != "\x00test024\x00test024-secret" {
					report.err = errors.New("incorrect authentication exchange")
					return
				}
				if behavior.reject == "AUTH" {
					if !write("535 rejected authentication") {
						return
					}
					continue
				}
				report.auth = true
				if !write("235 authenticated") {
					return
				}
			case "MAIL", "RCPT":
				if behavior.reject == verb {
					if !write("550 controlled rejection") {
						return
					}
					continue
				}
				if !write("250 accepted") {
					return
				}
			case "DATA":
				if behavior.reject == "DATA" {
					if !write("451 controlled data refusal") {
						return
					}
					continue
				}
				if !write("354 send data") {
					return
				}
				report.message, report.err = textproto.NewReader(reader).ReadDotBytes()
				if report.err != nil {
					return
				}
				if behavior.reject == "COMMIT" {
					if !write("451 not queued") {
						return
					}
					continue
				}
				if !write("250 queued") {
					return
				}
			case "QUIT":
				if behavior.reject == "QUIT" {
					write("550 controlled quit refusal")
				} else {
					write("221 bye")
				}
				return
			default:
				report.err = fmt.Errorf("unexpected SMTP verb %q", verb)
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		lock.Lock()
		if active != nil {
			_ = active.Close()
		}
		lock.Unlock()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("owned SMTP peer did not exit")
		}
	})
	return listener.Addr().(*net.TCPAddr).Port, result
}

func test024SMTPConfig(port int) config.SMTPConfig {
	return config.SMTPConfig{Enabled: true, Host: "localhost", Port: port, Username: "test024", Password: "test024-secret", From: "MCMods <sender@example.test>"}
}

func receiveTEST024SMTP(t *testing.T, result <-chan test024SMTPResult) test024SMTPResult {
	t.Helper()
	select {
	case report := <-result:
		return report
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP peer must finish after Send")
		return test024SMTPResult{}
	}
}

func assertTEST024SMTPMessage(t *testing.T, report test024SMTPResult, wantTLS bool) {
	t.Helper()
	if report.err != nil || !report.auth || (wantTLS && report.tls < tls.VersionTLS12) {
		t.Fatalf("protocol result auth=%v TLS=%x err=%v", report.auth, report.tls, report.err)
	}
	commands := strings.Join(report.commands, "\n")
	if !strings.Contains(commands, "MAIL FROM:<sender@example.test>") || !strings.Contains(commands, "RCPT TO:<recipient@example.test>") {
		t.Fatalf("wrong envelope: %s", commands)
	}
	message, err := mail.ReadMessage(strings.NewReader(string(report.message)))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil || subject != "站点邮件测试" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	body, err := io.ReadAll(message.Body)
	if err != nil || string(body) != ".first\n第二行\n" {
		t.Fatalf("dot-stuffed UTF8 body=%q err=%v", body, err)
	}
	if message.Header.Get("Content-Type") != `text/plain; charset="UTF-8"` || message.Header.Get("MIME-Version") != "1.0" {
		t.Fatal("MIME contract missing")
	}
}

func TestTEST024SMTPAuthenticatedDeliveryAndProtocolFailures(t *testing.T) {
	port, result := newTEST024SMTP(t, test024SMTPBehavior{auth: true})
	if err := New(test024SMTPConfig(port)).Send("Recipient <recipient@example.test>", "站点邮件测试", ".first\r\n第二行\r\n"); err != nil {
		t.Fatal(err)
	}
	assertTEST024SMTPMessage(t, receiveTEST024SMTP(t, result), false)
	for _, stage := range []string{"AUTH", "MAIL", "RCPT", "DATA", "COMMIT", "QUIT"} {
		t.Run(stage, func(t *testing.T) {
			port, result := newTEST024SMTP(t, test024SMTPBehavior{auth: true, reject: stage})
			if err := New(test024SMTPConfig(port)).Send("recipient@example.test", "subject", "body"); err == nil {
				t.Fatalf("%s refusal must propagate", stage)
			}
			report := receiveTEST024SMTP(t, result)
			if stage == "AUTH" || stage == "MAIL" || stage == "RCPT" || stage == "DATA" {
				if len(report.message) != 0 {
					t.Fatal("rejected transaction submitted message data")
				}
			}
		})
	}
}

func TestTEST024SMTPRequiredCapabilitiesStopBeforeSecrets(t *testing.T) {
	for _, requireTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS_%v", requireTLS), func(t *testing.T) {
			port, result := newTEST024SMTP(t, test024SMTPBehavior{})
			cfg := test024SMTPConfig(port)
			cfg.UseTLS = requireTLS
			err := New(cfg).Send("recipient@example.test", "subject", "body")
			want := "authentication"
			if requireTLS {
				want = "STARTTLS"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("missing required %s: %v", want, err)
			}
			report := receiveTEST024SMTP(t, result)
			for _, command := range report.commands {
				if strings.HasPrefix(command, "AUTH") || strings.HasPrefix(command, "MAIL") {
					t.Fatal("secret/data sent before required capability")
				}
			}
		})
	}
}

func test024SMTPCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(24), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, der
}

func TestTEST024SMTPSTARTTLSRejectsUntrustedCertificateBeforeAUTH(t *testing.T) {
	certificate, _ := test024SMTPCertificate(t)
	port, result := newTEST024SMTP(t, test024SMTPBehavior{auth: true, tlsConfig: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}})
	cfg := test024SMTPConfig(port)
	cfg.UseTLS = true
	err := New(cfg).Send("recipient@example.test", "subject", "body")
	var verification *tls.CertificateVerificationError
	if !errors.As(err, &verification) {
		t.Fatalf("expected actual certificate rejection, got %v", err)
	}
	report := receiveTEST024SMTP(t, result)
	if !strings.Contains(strings.Join(report.commands, "\n"), "STARTTLS") || report.auth || len(report.message) != 0 {
		t.Fatal("TLS failure must precede authentication/data")
	}
}

func TestTEST024SMTPSTARTTLSAuthenticatesOnlyAfterVerifiedTLS(t *testing.T) {
	if encoded := os.Getenv("MCMODS_TEST024_SMTP_ROOT"); encoded != "" {
		// Official x509 fallback roots are confined to this fresh test process.
		// No system trust mutation, production TLS hook or InsecureSkipVerify.
		der, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		root, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(root)
		x509.SetFallbackRoots(roots)
		port, err := strconv.Atoi(os.Getenv("MCMODS_TEST024_SMTP_PORT"))
		if err != nil {
			t.Fatal(err)
		}
		cfg := test024SMTPConfig(port)
		cfg.UseTLS = true
		if err = New(cfg).Send("Recipient <recipient@example.test>", "站点邮件测试", ".first\r\n第二行\r\n"); err != nil {
			t.Fatal(err)
		}
		return
	}
	certificate, der := test024SMTPCertificate(t)
	port, result := newTEST024SMTP(t, test024SMTPBehavior{auth: true, tlsConfig: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestTEST024SMTPSTARTTLSAuthenticatesOnlyAfterVerifiedTLS$", "-test.count=1")
	command.Env = append(os.Environ(), "GODEBUG="+os.Getenv("GODEBUG")+",x509usefallbackroots=1", "MCMODS_TEST024_SMTP_ROOT="+base64.StdEncoding.EncodeToString(der), "MCMODS_TEST024_SMTP_PORT="+strconv.Itoa(port))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("verified TLS child: %v\n%s", err, output)
	}
	report := receiveTEST024SMTP(t, result)
	assertTEST024SMTPMessage(t, report, true)
	commands := strings.Join(report.commands, "\n")
	if strings.Index(commands, "STARTTLS") >= strings.Index(commands, "AUTH") || strings.Count(commands, "EHLO ") != 2 {
		t.Fatalf("must renegotiate capabilities after TLS before AUTH: %s", commands)
	}
}

func TestTEST024SMTPDisabledAndMalformedHeadersNeverDial(t *testing.T) {
	port, result := newTEST024SMTP(t, test024SMTPBehavior{auth: true})
	for _, kind := range []string{"disabled", "from", "to", "subject"} {
		cfg := test024SMTPConfig(port)
		recipient, subject := "recipient@example.test", "subject"
		switch kind {
		case "disabled":
			cfg.Enabled = false
		case "from":
			cfg.From = "sender@example.test\r\nBcc: other@example.test"
		case "to":
			recipient = "recipient@example.test\nBcc: other@example.test"
		case "subject":
			subject += "\r\nBcc: other@example.test"
		}
		if err := New(cfg).Send(recipient, subject, "body"); err == nil {
			t.Fatalf("%s input accepted", kind)
		}
	}
	select {
	case <-result:
		t.Fatal("invalid mail contacted SMTP")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTEST024SMTPRealIODeadline(t *testing.T) {
	port, result := newTEST024SMTP(t, test024SMTPBehavior{stall: true})
	started := time.Now()
	err := New(test024SMTPConfig(port)).Send("recipient@example.test", "subject", "body")
	var networkError net.Error
	elapsed := time.Since(started)
	if !errors.As(err, &networkError) || !networkError.Timeout() || elapsed < 29*time.Second || elapsed > 35*time.Second {
		t.Fatalf("actual 30s I/O deadline: elapsed=%s error=%v", elapsed, err)
	}
	_ = receiveTEST024SMTP(t, result)
}
