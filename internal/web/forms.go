package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"benevole-ape/internal/model"
)

const defaultColor = "#3d7cc9"

// palette gives each new stand a distinct default colour.
var palette = []string{"#3d7cc9", "#d9534f", "#2e9e6a", "#e8932c", "#8a5cc7", "#c94f8f", "#2aa3b5", "#7a8b2e"}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validColor(s string) bool { return colorRe.MatchString(s) }

// clean trims, strips control characters (newlines kept if multiline) and
// caps the length in runes.
func clean(s string, max int, multiline bool) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == '\r' {
			continue
		}
		if unicode.IsControl(r) && !(multiline && r == '\n') {
			r = ' '
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// formInt parses an optional integer field; empty means def.
func formInt(r *http.Request, key string, def int) (int, error) {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 1000 {
		return 0, errors.New("nombre invalide : " + v)
	}
	return n, nil
}

// now is the clock used to validate dates (replaced in tests).
var now = time.Now

// checkDateRange accepts dates from one year ago to two years ahead. Without
// a limit, a far-future date would keep an event (and its personal data)
// away from the automatic cleanup forever.
func checkDateRange(t time.Time) error {
	today := now()
	min := time.Date(today.Year()-1, today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	max := time.Date(today.Year()+2, today.Month(), today.Day(), 23, 59, 59, 0, time.UTC)
	if t.Before(min) || t.After(max) {
		return fmt.Errorf("date hors limites : choisissez une date entre le %s et le %s", dateFR(min.Format(model.DateLayout)), dateFR(max.Format(model.DateLayout)))
	}
	return nil
}

func parseDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(model.DateLayout, s)
	if err != nil {
		return "", errors.New("date invalide (format attendu : AAAA-MM-JJ)")
	}
	if err := checkDateRange(t); err != nil {
		return "", err
	}
	return s, nil
}

// parseDateTime combines a YYYY-MM-DD date and an HH:MM time.
func parseDateTime(date, hhmm string) (time.Time, error) {
	t, err := time.Parse(model.TimeLayout, strings.TrimSpace(date)+" "+strings.TrimSpace(hhmm))
	if err != nil {
		return time.Time{}, errors.New("date ou heure invalide")
	}
	if err := checkDateRange(t); err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// volunteerForm is what a volunteer types in the sign-up form.
type volunteerForm struct {
	Name    string
	Email   string
	Phone   string
	Comment string
}

func readVolunteerForm(r *http.Request) volunteerForm {
	return volunteerForm{
		Name:    clean(r.FormValue("name"), 80, false),
		Email:   clean(r.FormValue("email"), 120, false),
		Phone:   clean(r.FormValue("phone"), 30, false),
		Comment: clean(r.FormValue("comment"), 300, false),
	}
}

var phoneRe = regexp.MustCompile(`^[0-9+ .()-]{6,30}$`)

// emailRe is deliberately strict: a plain address, without the characters
// (?, &, =, #, %, /, quotes, spaces…) that could add headers such as a BCC
// to the organiser's mailto: link.
var emailRe = regexp.MustCompile(`^[A-Za-z0-9._+'-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$`)

func validEmail(s string) bool {
	if !emailRe.MatchString(s) {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

func (f volunteerForm) validate(requireContact bool) error {
	if f.Name == "" {
		return errors.New("Merci d'indiquer votre nom.")
	}
	if f.Email != "" && !validEmail(f.Email) {
		return errors.New("L'adresse e-mail ne semble pas valide.")
	}
	if f.Phone != "" && !phoneRe.MatchString(f.Phone) {
		return errors.New("Le numéro de téléphone ne semble pas valide.")
	}
	if requireContact && f.Email == "" && f.Phone == "" {
		return errors.New("Merci d'indiquer un e-mail ou un téléphone, pour que l'APE puisse vous joindre.")
	}
	return nil
}
