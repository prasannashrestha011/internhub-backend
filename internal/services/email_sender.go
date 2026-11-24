package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type VerificationSender interface {
	SendVerification(context.Context, string, string) error
}

type SMTPVerificationSender struct{ Config config.EmailConfig }

func (s SMTPVerificationSender) SendVerification(ctx context.Context, to, code string) error {
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
		return fmt.Errorf("invalid verification code")
	}
	return s.send(ctx, to, "Confirm your Student Job Portal account", fmt.Sprintf("Your confirmation code is: %s\r\n\r\nThis code expires in 10 minutes. Only the latest code works.\r\nEnter it only if you created this account. Do not share it.\r\nIf you did not create this account, ignore this email.\r\n", code))
}

type ApplicationStatusSender interface {
	SendApplicationStatus(context.Context, string, string, models.InternshipApplicationStatus, string) error
}

func (s SMTPVerificationSender) SendApplicationStatus(ctx context.Context, to, title string, status models.InternshipApplicationStatus, note string) error {
	if status != models.ApplicationStatusAccepted && status != models.ApplicationStatusRejected {
		return fmt.Errorf("invalid application decision")
	}
	body := fmt.Sprintf("Your application for %s has been %s.\r\n", title, status)
	if note != "" {
		body += "\r\nMessage from recruiter:\r\n" + note + "\r\n"
	}
	body += "\r\nSign in to Student Job Portal to view your applications.\r\n"
	return s.send(ctx, to, "Your application status has changed", body)
}

func (s SMTPVerificationSender) send(ctx context.Context, to, subject, body string) error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("invalid recipient")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.Config.Host, s.Config.Port))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	tlsConfig := &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
	if s.Config.Port == "465" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if s.Config.Port == "587" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if err := client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
		return err
	}
	if err := client.Mail(s.Config.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", s.Config.From, to, subject, time.Now().UTC().Format(time.RFC1123Z), body)
	if _, err := writer.Write([]byte(message)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	// DATA acceptance is success even if the server closes before QUIT.
	_ = client.Quit()
	return nil
}
