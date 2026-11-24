package config

import (
	"strings"
	"testing"
)

func TestEmailConfigRequiresTLSAndSafeAddresses(t *testing.T) {
	valid := EmailConfig{Host: "smtp.example.com", Port: "587", Username: "user", Password: "password", From: "no-reply@example.com", OTPSecret: strings.Repeat("secret", 6)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.Port = "465"
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*EmailConfig){
		func(c *EmailConfig) { c.Port = "25" },
		func(c *EmailConfig) { c.From = "no-reply@example.com\r\nBcc: victim@example.com" },
		func(c *EmailConfig) { c.Host = "smtp.example.com:25" },
		func(c *EmailConfig) { c.Password = "" },
		func(c *EmailConfig) { c.OTPSecret = "short" },
	} {
		invalid := valid
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid mail configuration accepted")
		}
	}
}
