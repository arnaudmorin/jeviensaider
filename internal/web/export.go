package web

import (
	"encoding/csv"
	"fmt"
	"net/http"

	"benevole-ape/internal/model"
	"benevole-ape/internal/store"
)

// adminExportCSV writes the full planning as CSV: one line per sign-up, plus
// one line per missing person so empty places are visible too. Semicolon
// separator and UTF-8 BOM so that French Excel opens it directly.
func (s *Server) adminExportCSV(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	p, err := s.planning(r.Context(), ev, 0)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="benevoles-%s.csv"`, store.Slugify(ev.Title)))
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.Write([]string{"Stand", "Date", "Début", "Fin", "Nom", "E-mail", "Téléphone", "Commentaire", "Inscrit le"})
	for _, st := range p.Stands {
		for _, sl := range st.Slots {
			base := []string{st.Name, sl.StartsAt.Format("02/01/2006"), sl.StartsAt.Format("15:04"), sl.EndsAt.Format("15:04")}
			for _, su := range sl.Signups {
				cw.Write(append(base[:4:4], csvSafe(su.Name), csvSafe(su.Email), csvSafe(su.Phone), csvSafe(su.Comment),
					su.CreatedAt.Local().Format("02/01/2006 15:04")))
			}
			for i := 0; i < sl.Missing; i++ {
				cw.Write(append(base[:4:4], "(place libre)", "", "", "", ""))
			}
		}
	}
	cw.Flush()
}

// csvSafe neutralises spreadsheet formula injection (=, +, -, @ prefixes).
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		return "'" + s
	}
	return s
}
