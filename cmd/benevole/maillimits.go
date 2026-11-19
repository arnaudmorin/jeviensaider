package main

import (
	"fmt"
	"strconv"
	"strings"

	"jeviensaider/internal/web"
)

// mailLimitsFromEnv reads the e-mail limits. Each variable is a number, or
// 0 / off / unlimited for no limit; unset means the default. Anything else
// is an error rather than a silent fallback.
//
//	BENEVOLE_MAIL_CREATED_PER_ADDRESS_PER_DAY  "event created" mails to one address (default 3)
//	BENEVOLE_MAIL_CREATED_PER_HOUR             "event created" mails in all (default 30)
//	BENEVOLE_MAIL_SIGNUPS_PER_EVENT_PER_HOUR   sign-up notifications per event (default 20)
//	BENEVOLE_MAIL_PER_HOUR                     all mails (default 300)
func mailLimitsFromEnv() (web.MailLimits, error) {
	l := web.DefaultMailLimits()
	for _, v := range []struct {
		name string
		dst  *int
	}{
		{"BENEVOLE_MAIL_CREATED_PER_ADDRESS_PER_DAY", &l.CreatedPerAddressPerDay},
		{"BENEVOLE_MAIL_CREATED_PER_HOUR", &l.CreatedPerHour},
		{"BENEVOLE_MAIL_SIGNUPS_PER_EVENT_PER_HOUR", &l.SignupsPerEventPerHour},
		{"BENEVOLE_MAIL_PER_HOUR", &l.PerHour},
	} {
		s := strings.ToLower(strings.TrimSpace(env(v.name, "")))
		switch s {
		case "":
			continue
		case "0", "off", "none", "unlimited", "no":
			*v.dst = 0
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return l, fmt.Errorf("%s=%q: use a number, or 0 / unlimited for no limit", v.name, s)
		}
		*v.dst = n
	}
	return l, nil
}
