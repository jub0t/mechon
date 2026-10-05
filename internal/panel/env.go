package panel

import (
	"context"
	"net/http"
	"regexp"
	"sort"

	"github.com/google/uuid"

	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/templates"
)

// Environment variables are stored encrypted. Secret ones are write-only: the API never returns
// their values, and saving one with an empty value keeps what was there.

type envVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// Names the platform sets itself, which bots may not override.
var reservedEnv = map[string]bool{"HOME": true, "DATA_DIR": true, "PATH": true, "HOSTNAME": true}

const maxEnvVars = 100
const maxEnvValue = 32 << 10

func validateEnv(vars []envVar) error {
	if len(vars) > maxEnvVars {
		return errBadRequest("A bot can have at most 100 environment variables.")
	}
	seen := map[string]bool{}
	for _, v := range vars {
		if !envKeyRe.MatchString(v.Key) {
			return errBadRequest("\"" + v.Key + "\" is not a valid variable name (letters, digits and _).")
		}
		if reservedEnv[v.Key] {
			return errBadRequest(v.Key + " is set by Mechon and cannot be changed.")
		}
		if seen[v.Key] {
			return errBadRequest(v.Key + " is listed twice.")
		}
		seen[v.Key] = true
		if len(v.Value) > maxEnvValue {
			return errBadRequest(v.Key + " is too long.")
		}
	}
	return nil
}

// envFromMap turns the create-bot form into variables, marking the template's secret keys secret
// and checking its required ones.
func envFromMap(m map[string]string, tpl templates.Template) ([]envVar, error) {
	secret := map[string]bool{}
	for _, e := range tpl.Env {
		secret[e.Key] = e.Secret
		if e.Required && m[e.Key] == "" {
			return nil, errBadRequest(e.Label + " is required.")
		}
	}
	vars := make([]envVar, 0, len(m))
	for k, v := range m {
		if v == "" {
			continue
		}
		vars = append(vars, envVar{Key: k, Value: v, Secret: secret[k]})
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Key < vars[j].Key })
	return vars, validateEnv(vars)
}

func envAD(botID uuid.UUID, key string) []byte { return []byte(botID.String() + "/" + key) }

func (s *Server) writeEnv(ctx context.Context, q *db.Queries, botID uuid.UUID, vars []envVar) error {
	if err := q.DeleteBotEnv(ctx, botID); err != nil {
		return err
	}
	for _, v := range vars {
		if err := q.InsertBotEnv(ctx, db.InsertBotEnvParams{BotID: botID, Key: v.Key, ValueEnc: s.box.Seal([]byte(v.Value), envAD(botID, v.Key)), Secret: v.Secret}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) readEnv(ctx context.Context, botID uuid.UUID) ([]envVar, error) {
	rows, err := s.q.ListBotEnv(ctx, botID)
	if err != nil {
		return nil, err
	}
	out := make([]envVar, 0, len(rows))
	for _, r := range rows {
		pt, err := s.box.Open(r.ValueEnc, envAD(botID, r.Key))
		if err != nil {
			return nil, err
		}
		out = append(out, envVar{Key: r.Key, Value: string(pt), Secret: r.Secret})
	}
	return out, nil
}

func (s *Server) decryptEnv(ctx context.Context, botID uuid.UUID) (map[string]string, error) {
	vars, err := s.readEnv(ctx, botID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Key] = v.Value
	}
	return m, nil
}

func (s *Server) getEnv(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	vars, err := s.readEnv(r.Context(), row.Bot.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for i := range vars {
		if vars[i].Secret {
			vars[i].Value = ""
		}
	}
	writeJSON(w, http.StatusOK, vars)
}

// putEnv replaces the whole set. The bot restarts with the new values.
func (s *Server) putEnv(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Vars []envVar `json:"vars"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := validateEnv(in.Vars); err != nil {
		writeError(w, r, err)
		return
	}
	ctx := r.Context()
	old, err := s.readEnv(ctx, row.Bot.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	prev := map[string]envVar{}
	for _, v := range old {
		prev[v.Key] = v
	}
	vars := in.Vars[:0]
	for _, v := range in.Vars {
		if v.Secret && v.Value == "" {
			p, ok := prev[v.Key]
			if !ok || !p.Secret {
				writeError(w, r, errBadRequest("Enter a value for "+v.Key+"."))
				return
			}
			v.Value = p.Value // unchanged secret
		}
		vars = append(vars, v)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err := s.writeEnv(ctx, s.q.WithTx(tx), row.Bot.ID, vars); err != nil {
		writeError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, r, err)
		return
	}
	keys := make([]string, 0, len(vars))
	for _, v := range vars {
		keys = append(keys, v.Key)
	}
	s.audit(r, "bot.env", "bot", row.Bot.ID.String(), row.Bot.Name, map[string]any{"keys": keys})
	w.WriteHeader(http.StatusNoContent)
	s.hub.pushBot(context.WithoutCancel(ctx), row.Bot.ID)
}
