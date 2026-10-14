package main

import (
	"strings"
	"testing"
)

func TestHelpLocalePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, all, messages, lang, want string
	}{
		{"supported LC_ALL wins", "ru_RU.UTF-8", "ja", "de", "ru"},
		{"C LC_ALL stops lookup", "C", "pt", "ja", ""},
		{"POSIX LC_ALL stops lookup", "POSIX.UTF-8", "pt", "ja", ""},
		{"unsupported LC_ALL stops lookup", "de_DE.UTF-8", "pt", "ja", ""},
		{"C LC_MESSAGES stops lookup", "", "C", "ja", ""},
		{"POSIX LC_MESSAGES stops lookup", "", "POSIX", "ja", ""},
		{"unsupported LC_MESSAGES stops lookup", "", "de_DE.UTF-8", "ja", ""},
		{"empty LC_ALL falls through", "", "ja", "es", "ja"},
		{"empty higher variables fall through", "", "", "es_MX", "es"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.all)
			t.Setenv("LC_MESSAGES", tc.messages)
			t.Setenv("LANG", tc.lang)
			if got := helpLocale(); got != tc.want {
				t.Fatalf("helpLocale() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHelpUsageTranslated(t *testing.T) {
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
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
	t.Setenv("LC_MESSAGES", "ja_JP.UTF-8")
	t.Setenv("LANG", "ru_RU.UTF-8")
	if got := help(usage); got != usage {
		t.Fatal("LC_ALL=C must stay English despite lower priority locale values")
	}
}
