package libtorrent

import (
	"log"
	"regexp"
	"testing"
)

func TestWildcast(t *testing.T) {
	m := regexp.MustCompile(wildcardToRegex("*.mp3"))
	var s string
	s = "test/abc/123.mp3"
	log.Println(s, m.MatchString(s))
	s = "123.mp3"
	log.Println(s, m.MatchString(s))
	s = "123.mp4"
	log.Println(s, m.MatchString(s))
	m2 := regexp.MustCompile(wildcardToRegex("*64kb.mp3"))
	s = "test/abc/123_64kb.mp3"
	log.Println(wildcardToRegex("*64kb.mp3"), s, m2.MatchString(s))
}
