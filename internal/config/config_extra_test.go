package config

import (
	"os"
	"testing"
)

// TestExpandHomeErrorWhenHomeUnset exercises the os.UserHomeDir() failure branch
// inside expandHome: when HOME is unset on Linux, UserHomeDir returns an error.
func TestExpandHomeErrorWhenHomeUnset(t *testing.T) {
	orig, hadOrig := os.LookupEnv("HOME")
	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatalf("unsetenv HOME: %v", err)
	}
	t.Cleanup(func() {
		if hadOrig {
			if err := os.Setenv("HOME", orig); err != nil {
				t.Errorf("restore HOME: %v", err)
			}
		} else {
			_ = os.Unsetenv("HOME")
		}
	})

	_, err := expandHome("~/somepath")
	if err == nil {
		t.Error("expandHome with tilde path and no HOME should return an error")
	}
}

// TestResolveExpandHomeFails exercises the Resolve error path when expandHome
// fails (dir starts with ~ but HOME is unset).
func TestResolveExpandHomeFails(t *testing.T) {
	orig, hadOrig := os.LookupEnv("HOME")
	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatalf("unsetenv HOME: %v", err)
	}
	t.Cleanup(func() {
		if hadOrig {
			if err := os.Setenv("HOME", orig); err != nil {
				t.Errorf("restore HOME: %v", err)
			}
		} else {
			_ = os.Unsetenv("HOME")
		}
	})

	_, err := Resolve("~/repos", 5, "tok")
	if err == nil {
		t.Error("Resolve with tilde dir and no HOME should return an error")
	}
}

// TestTokenFromGHHostsHomeUnset exercises the os.UserHomeDir() failure branch
// inside tokenFromGHHosts.
func TestTokenFromGHHostsHomeUnset(t *testing.T) {
	orig, hadOrig := os.LookupEnv("HOME")
	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatalf("unsetenv HOME: %v", err)
	}
	t.Cleanup(func() {
		if hadOrig {
			if err := os.Setenv("HOME", orig); err != nil {
				t.Errorf("restore HOME: %v", err)
			}
		} else {
			_ = os.Unsetenv("HOME")
		}
	})

	_, _, err := tokenFromGHHosts()
	if err == nil {
		t.Error("tokenFromGHHosts with no HOME should return an error")
	}
}
