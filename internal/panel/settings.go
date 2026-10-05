package panel

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/jub0t/mechon/internal/db"
)

// Install-wide settings an operator can change from the panel. Only these keys exist.
type siteSettings struct {
	BrandName  string `json:"brandName"`  // shown on the sign-in page and in the browser tab
	SupportURL string `json:"supportUrl"` // where "Contact support" links go
}

func (s *Server) loadSettings(ctx context.Context) (siteSettings, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return siteSettings{}, err
	}
	var out siteSettings
	for _, r := range rows {
		switch r.Key {
		case "brand_name":
			out.BrandName = r.Value
		case "support_url":
			out.SupportURL = r.Value
		}
	}
	return out, nil
}

// publicSettings needs no sign-in: the login page uses it.
func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.loadSettings(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in siteSettings
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	in.BrandName = strings.TrimSpace(in.BrandName)
	in.SupportURL = strings.TrimSpace(in.SupportURL)
	if len([]rune(in.BrandName)) > 40 {
		writeError(w, r, errBadRequest("Keep the brand name under 40 characters."))
		return
	}
	if in.SupportURL != "" {
		u, err := url.Parse(in.SupportURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
			writeError(w, r, errBadRequest("Support link must start with https://, http:// or mailto:."))
			return
		}
	}
	ctx := r.Context()
	for k, v := range map[string]string{"brand_name": in.BrandName, "support_url": in.SupportURL} {
		if err := s.q.PutSetting(ctx, db.PutSettingParams{Key: k, Value: v}); err != nil {
			writeError(w, r, err)
			return
		}
	}
	s.audit(r, "settings.update", "settings", "install", "", map[string]any{"brandName": in.BrandName, "supportUrl": in.SupportURL})
	writeJSON(w, http.StatusOK, in)
}
