package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"benevole-ape/internal/mail"
	"benevole-ape/internal/store"
	"benevole-ape/internal/web"
)

// config is the server's configuration, read from flags and environment
// variables (a flag overrides its variable).
type config struct {
	addr           string
	dbPath         string
	baseURL        string // without a trailing slash
	trustProxy     bool
	retentionDays  int             // 0: events are kept forever
	retentionMax   int             // longest period an organiser can choose, 0: not set (see newRetention)
	createPassword string          // empty: anyone can create events
	retention      store.Retention // built from the two retention values above
	smtp           *mail.Config
	mailLimits     web.MailLimits
	healthcheck    bool // only check a running server's health and exit
}

const usage = `Usage:
  benevole [flags]           run the web server
  benevole events <command>  super-admin tool: list, show, delete events (see "benevole events help")
  benevole mail-test <addr>  send a test e-mail with the SMTP settings

Flags:
`

// loadConfig reads the server's configuration. Any invalid value is an
// error: nothing silently falls back to a default.
func loadConfig(args []string) (*config, error) {
	trustProxy, err := boolFromEnv("BENEVOLE_TRUST_PROXY", false)
	if err != nil {
		return nil, err
	}
	retention, err := retentionFromEnv()
	if err != nil {
		return nil, err
	}
	retentionMax, err := retentionMaxFromEnv()
	if err != nil {
		return nil, err
	}

	c := &config{}
	fs := flag.NewFlagSet("benevole", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	fs.StringVar(&c.addr, "addr", env("BENEVOLE_ADDR", ":8080"), "listen address (env BENEVOLE_ADDR)")
	fs.StringVar(&c.dbPath, "db", env("BENEVOLE_DB", "benevole.db"), "SQLite database file (env BENEVOLE_DB)")
	fs.StringVar(&c.baseURL, "base-url", env("BENEVOLE_BASE_URL", ""), "public URL, e.g. https://benevoles.example.org (env BENEVOLE_BASE_URL)")
	fs.BoolVar(&c.trustProxy, "trust-proxy", trustProxy, "trust X-Forwarded-* headers from a reverse proxy (env BENEVOLE_TRUST_PROXY)")
	fs.IntVar(&c.retentionDays, "retention-days", retention, "delete events this many days after their last day of activity when the organiser chose none, 0 to keep them forever (env BENEVOLE_RETENTION_DAYS)")
	fs.IntVar(&c.retentionMax, "retention-max-days", retentionMax, "longest period an organiser can choose, 0 for the default of 180 days or the value of -retention-days if that is longer (env BENEVOLE_RETENTION_MAX_DAYS)")
	fs.BoolVar(&c.healthcheck, "healthcheck", false, "query /healthz on the listen address and exit 0 if healthy (for container health checks)")
	fs.Parse(args)
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	if c.retention, err = newRetention(c.retentionDays, c.retentionMax); err != nil {
		return nil, err
	}

	// The passwords are only read from the environment, so that they never
	// show up in `ps` output.
	c.createPassword = os.Getenv("BENEVOLE_CREATE_PASSWORD")
	if c.smtp, err = smtpFromEnv(c.baseURL); err != nil {
		return nil, err
	}
	if err := checkOpenCreation(c.smtp, c.createPassword); err != nil {
		return nil, err
	}
	if c.mailLimits, err = mailLimitsFromEnv(); err != nil {
		return nil, err
	}
	return c, nil
}

// boolFromEnv reads a true/false environment variable; unset or empty means
// def.
func boolFromEnv(key string, def bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s=%q: use true or false", key, v)
	}
	return b, nil
}
