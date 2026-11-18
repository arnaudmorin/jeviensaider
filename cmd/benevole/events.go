package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"benevole-ape/internal/model"
	"benevole-ape/internal/store"
)

const eventsUsage = `Usage: benevole events <command> [flags] [args]

Super-admin tool. It works directly on the SQLite database, so it can see and
change every event; it is safe to run while the server is running.

Commands:
  list                  list all events, most recent first
  show <event>          details and the public and admin links of an event
  delete <event>...     delete events with all their data (asks first)

<event> is an event's slug (as in /e/<slug>) or its ID from "list".

Flags:
  -db FILE              SQLite database (env BENEVOLE_DB, default benevole.db)
  -base-url URL         public URL used to print links (env BENEVOLE_BASE_URL)
  -retention-days N     retention used to compute expiry dates, 0 = kept forever (env BENEVOLE_RETENTION_DAYS, default 60)
  -y                    delete: do not ask for confirmation
`

type eventsCmd struct {
	stdin     *bufio.Reader
	out       io.Writer
	db        string
	baseURL   string
	retention int
	yes       bool
}

// runEvents implements "benevole events ..." and returns the exit code.
func runEvents(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	c := &eventsCmd{stdin: bufio.NewReader(stdin), out: stdout}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		fmt.Fprint(stdout, eventsUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("benevole events "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, eventsUsage) }
	fs.StringVar(&c.db, "db", env("BENEVOLE_DB", "benevole.db"), "")
	fs.StringVar(&c.baseURL, "base-url", env("BENEVOLE_BASE_URL", ""), "")
	retention, err := retentionFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fs.IntVar(&c.retention, "retention-days", retention, "")
	fs.BoolVar(&c.yes, "y", false, "")
	pos, err := parseInterspersed(fs, rest)
	if err != nil {
		return 2
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")

	var run func(context.Context, *store.Store, []string) error
	switch sub {
	case "list", "ls":
		run = c.list
	case "show":
		run = c.show
	case "delete", "rm":
		run = c.delete
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", sub, eventsUsage)
		return 2
	}

	st, err := store.Open(c.db)
	if err != nil {
		fmt.Fprintf(stderr, "open %s: %v\n", c.db, err)
		return 1
	}
	defer st.Close()
	if err := run(context.Background(), st, pos); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// parseInterspersed parses flags placed anywhere among the positional
// arguments ("delete my-event -y" as well as "delete -y my-event").
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (c *eventsCmd) expires(e store.EventSummary) string {
	if c.retention <= 0 {
		return "never"
	}
	return store.ExpiresOn(e.LastDay, c.retention).Format(model.DateLayout)
}

func (c *eventsCmd) list(ctx context.Context, st *store.Store, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("list takes no arguments")
	}
	events, err := st.ListEventSummaries(ctx)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Fprintln(c.out, "No events.")
		return nil
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSLUG\tTITLE\tDATE\tSTATUS\tSTANDS\tSLOTS\tVOLUNTEERS\tEXPIRES")
	for _, e := range events {
		date := e.Date
		if date == "" {
			date = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			e.ID, e.Slug, truncate(e.Title, 40), date, e.Status, e.Stands, e.Slots, e.Volunteers, c.expires(e))
	}
	return w.Flush()
}

func (c *eventsCmd) show(ctx context.Context, st *store.Store, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: benevole events show <event>")
	}
	events, err := st.ListEventSummaries(ctx)
	if err != nil {
		return err
	}
	e, err := find(events, args[0])
	if err != nil {
		return err
	}
	base := c.baseURL
	if base == "" {
		base = "(base URL)"
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID\t%d\n", e.ID)
	fmt.Fprintf(w, "Title\t%s\n", e.Title)
	fmt.Fprintf(w, "Date\t%s\n", orDash(e.Date))
	fmt.Fprintf(w, "Location\t%s\n", orDash(e.Location))
	fmt.Fprintf(w, "Status\t%s\n", e.Status)
	fmt.Fprintf(w, "Created\t%s\n", e.CreatedAt.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "Content\t%d stands, %d slots, %d volunteers, %d sign-ups\n", e.Stands, e.Slots, e.Volunteers, e.Signups)
	fmt.Fprintf(w, "Last day\t%s\n", e.LastDay.Format(model.DateLayout))
	fmt.Fprintf(w, "Expires\t%s\n", c.expires(e))
	fmt.Fprintf(w, "Public link\t%s/e/%s\n", base, e.Slug)
	fmt.Fprintf(w, "Admin link\t%s/a/%s\n", base, e.AdminToken)
	return w.Flush()
}

func (c *eventsCmd) delete(ctx context.Context, st *store.Store, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: benevole events delete [-y] <event>...")
	}
	events, err := st.ListEventSummaries(ctx)
	if err != nil {
		return err
	}
	// Resolve every argument first, so a typo deletes nothing.
	var targets []store.EventSummary
	seen := map[int64]bool{}
	for _, ref := range args {
		e, err := find(events, ref)
		if err != nil {
			return err
		}
		if !seen[e.ID] {
			seen[e.ID] = true
			targets = append(targets, e)
		}
	}
	for _, e := range targets {
		fmt.Fprintf(c.out, "  %d  %s  %q  (%d stands, %d slots, %d volunteers)\n", e.ID, e.Slug, e.Title, e.Stands, e.Slots, e.Volunteers)
	}
	if !c.yes {
		fmt.Fprintf(c.out, "Delete %d event(s) and all their data? This cannot be undone. [y/N] ", len(targets))
		answer, _ := c.stdin.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" && a != "o" && a != "oui" {
			fmt.Fprintln(c.out, "Cancelled.")
			return nil
		}
	}
	for _, e := range targets {
		if err := st.DeleteEvent(ctx, e.ID); err != nil {
			return fmt.Errorf("delete %s: %w", e.Slug, err)
		}
		fmt.Fprintf(c.out, "Deleted %s\n", e.Slug)
	}
	return nil
}

// find resolves an event by slug, or by ID when ref is a number.
func find(events []store.EventSummary, ref string) (store.EventSummary, error) {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "/e/")
	id, idErr := strconv.ParseInt(ref, 10, 64)
	for _, e := range events {
		if e.Slug == ref || (idErr == nil && e.ID == id) {
			return e, nil
		}
	}
	return store.EventSummary{}, fmt.Errorf("no event %q (use a slug or an ID from \"benevole events list\")", ref)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
