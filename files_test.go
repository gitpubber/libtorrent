package libtorrent

import (
	"regexp"
	"testing"
)

func TestWildcast(t *testing.T) {
	var m *regexp.Regexp
	var s string

	m = regexp.MustCompile(wildcardToRegex("*.mp3"))
	s = "test/abc/123.mp3"
	if !m.MatchString(s) {
		t.Error(s, m.MatchString(s))
	}

	s = "123.mp3"
	if !m.MatchString(s) {
		t.Error(s, m.MatchString(s))
	}

	s = "123.mp4"
	if m.MatchString(s) {
		t.Error(s, m.MatchString(s))
	}

	m = regexp.MustCompile(wildcardToRegex("*64kb.mp3"))
	s = "test/abc/123_64kb.mp3"
	if !m.MatchString(s) {
		t.Error(wildcardToRegex("*64kb.mp3"), s, m.MatchString(s))
	}

	m = regexp.MustCompile(wildcardToRegex("test/*"))
	s = "test/abc/123_64kb.mp3"
	if !m.MatchString(s) {
		t.Error(s, m.MatchString(s))
	}

	m = regexp.MustCompile(wildcardToRegex("test/*"))
	s = "test2/abc/123_64kb.mp3"
	if m.MatchString(s) {
		t.Error(s, m.MatchString(s))
	}

	m = regexp.MustCompile(wildcardToRegex("test/*"))
	s = "test/abc/123_64kb.mp3"
	if !m.MatchString(s) {
		t.Error(s, m.MatchString(s))
	}
}
