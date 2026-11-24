package services

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/models"
)

func TestSMTPRejectsUnencryptedServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:587")
	if err != nil {
		t.Skipf("local SMTP submission port unavailable: %v", err)
	}
	defer listener.Close()
	commands := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			commands <- ""
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		reader := bufio.NewReader(conn)
		var transcript strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			transcript.WriteString(line)
			if strings.HasPrefix(line, "EHLO") {
				_, _ = fmt.Fprint(conn, "250 localhost\r\n")
			} else {
				_, _ = fmt.Fprint(conn, "454 TLS unavailable\r\n")
			}
		}
		commands <- transcript.String()
	}()
	sender := SMTPVerificationSender{Config: config.EmailConfig{Host: "127.0.0.1", Port: "587", Username: "secret-user", Password: "secret-password", From: "no-reply@example.com", OTPSecret: strings.Repeat("test", 8)}}
	if err := sender.SendVerification(context.Background(), "recipient@example.com", "000123"); err == nil {
		t.Fatal("unencrypted SMTP server accepted")
	}
	transcript := <-commands
	if strings.Contains(transcript, "AUTH") || strings.Contains(transcript, "MAIL FROM") || strings.Contains(transcript, "000123") {
		t.Fatal("credentials or message sent before TLS")
	}
}

func TestApplicationStatusEmailValidation(t *testing.T) {
	sender := SMTPVerificationSender{Config: config.EmailConfig{Host: "smtp.example.com", Port: "587", Username: "test", Password: "test", From: "no-reply@example.com", OTPSecret: strings.Repeat("test", 8)}}
	for _, status := range []models.InternshipApplicationStatus{models.ApplicationStatusSubmitted, models.ApplicationStatusWithdrawn, "unknown"} {
		if err := sender.SendApplicationStatus(context.Background(), "student@example.com", "Intern", status, ""); err == nil {
			t.Fatalf("invalid decision %q accepted", status)
		}
	}
	for _, to := range []string{"invalid", "student@example.com\r\nBcc: other@example.com"} {
		if err := sender.SendApplicationStatus(context.Background(), to, "Intern", models.ApplicationStatusAccepted, ""); err == nil {
			t.Fatalf("invalid recipient %q accepted", to)
		}
	}
}
