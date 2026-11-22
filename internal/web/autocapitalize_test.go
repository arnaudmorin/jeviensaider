package web

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Phone keyboards do not all start a field with a capital letter. Every field
// where people type free text says how to capitalize: "words" for the name of
// a person, "sentences" for the rest, "none" for a unit that goes in the
// middle of a sentence. Fields that are not free text (e-mail, phone, number,
// password…), read-only fields and the hidden anti-spam field are left out.
func TestTextFieldsSayHowToCapitalize(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatal(err)
	}
	field := regexp.MustCompile(`(?s)<(input|textarea)\b[^>]*>`)
	notText := regexp.MustCompile(`type="(hidden|checkbox|radio|color|number|time|date|email|tel|url|submit|password)"|readonly|tabindex="-1"`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range field.FindAll(src, -1) {
			if notText.Match(tag) {
				continue
			}
			if !regexp.MustCompile(`autocapitalize="(words|sentences|none)"`).Match(tag) {
				t.Errorf("%s: %s has no autocapitalize", f, tag)
			}
		}
	}
}
