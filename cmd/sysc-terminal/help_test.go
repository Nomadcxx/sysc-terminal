package main

import (
	"strings"
	"testing"
)

func TestHelpLocalePrecedence(t *testing.T) {
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	t.Setenv("LC_MESSAGES", "ja")
	t.Setenv("LANG", "de")
	if got := helpLocale(); got != "ru" {
		t.Fatalf("helpLocale() = %q, want ru", got)
	}
	t.Setenv("LC_ALL", "")
	if got := helpLocale(); got != "ja" {
		t.Fatalf("helpLocale() = %q, want ja", got)
	}
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "es_MX")
	if got := helpLocale(); got != "es" {
		t.Fatalf("helpLocale() = %q, want es", got)
	}
	t.Setenv("LANG", "C")
	if got := helpLocale(); got != "" {
		t.Fatalf("helpLocale() = %q, want empty", got)
	}
}

func TestHelpUsageTranslated(t *testing.T) {
	markers := map[string]string{
		"es": "Uso:", "pt": "papel de parede", "ja": "使い方:", "ko": "사용법:", "ru": "Использование:",
	}
	for locale, marker := range markers {
		t.Setenv("LC_ALL", locale)
		got := help(usage)
		if got == usage {
			t.Fatalf("%s: usage unchanged", locale)
		}
		if !strings.Contains(got, marker) {
			t.Fatalf("%s: missing %q", locale, marker)
		}
		if strings.HasSuffix(got, "\n") != strings.HasSuffix(usage, "\n") {
			t.Fatalf("%s: newline parity", locale)
		}
	}
	t.Setenv("LC_ALL", "C")
	if got := help(usage); got != usage {
		t.Fatal("C locale must stay English")
	}
}
