package panel

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jub0t/mechon/internal/db"
)

const maxUploadBytes = 512 << 20

type deployJSON struct {
	ID            uuid.UUID       `json:"id"`
	Number        int32           `json:"number"`
	Source        db.DeploySource `json:"source"`
	Status        db.DeployStatus `json:"status"`
	Error         string          `json:"error"`
	SHA256        string          `json:"sha256"`
	Bytes         int64           `json:"bytes"`
	RollbackOf    *uuid.UUID      `json:"rollbackOf"`
	CreatedByName *string         `json:"createdBy"`
	GitURL        string          `json:"gitUrl,omitempty"`
	GitRef        string          `json:"gitRef,omitempty"`
	GitCommit     string          `json:"gitCommit,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	FinishedAt    *time.Time      `json:"finishedAt"`
	Current       bool            `json:"current"`
	Log           *string         `json:"log,omitempty"`
}

func toDeployJSON(d db.Deploy, by *string, current *uuid.UUID) deployJSON {
	return deployJSON{ID: d.ID, Number: d.Number, Source: d.Source, Status: d.Status, Error: d.Error, SHA256: d.ArtifactSha256,
		Bytes: d.ArtifactBytes, RollbackOf: d.RollbackOf, CreatedByName: by, CreatedAt: d.CreatedAt, FinishedAt: d.FinishedAt,
		GitURL: d.GitUrl, GitRef: d.GitRef, GitCommit: d.GitCommit,
		Current: current != nil && *current == d.ID}
}

func (s *Server) listDeploys(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := s.q.ListDeploys(r.Context(), row.Bot.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]deployJSON, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDeployJSON(db.Deploy{ID: d.ID, BotID: d.BotID, Number: d.Number, Source: d.Source, RollbackOf: d.RollbackOf,
			GitUrl: d.GitUrl, GitRef: d.GitRef, GitCommit: d.GitCommit,
			ArtifactSha256: d.ArtifactSha256, ArtifactBytes: d.ArtifactBytes, Status: d.Status, Error: d.Error,
			CreatedAt: d.CreatedAt, FinishedAt: d.FinishedAt}, d.CreatedByName, row.Bot.CurrentDeployID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDeploy(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := pathID(r, "deploy")
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := s.q.GetDeploy(r.Context(), id)
	if err != nil || d.BotID != row.Bot.ID {
		writeError(w, r, errNotFound)
		return
	}
	v := toDeployJSON(d, nil, row.Bot.CurrentDeployID)
	v.Log = &d.Log
	writeJSON(w, http.StatusOK, v)
}

// createDeploy takes either a multipart upload (field "file": a .zip or .tar.gz of the bot's
// code; the "api" source when an API key sends it) or a JSON body {"gitUrl", "gitRef", "token"}
// to deploy a branch or tag from a git repository over https.
func (s *Server) createDeploy(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit := int64(row.Bot.DiskMb) << 20
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		s.createGitDeploy(w, r, row.Bot.ID, limit)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, min(limit, maxUploadBytes)+(1<<20))
	file, _, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, r, errBadRequest("That upload is larger than this bot's disk."))
			return
		}
		writeError(w, r, errBadRequest("Attach the bot's code as a .zip or .tar.gz in the \"file\" field."))
		return
	}
	defer file.Close()
	art, err := storeUpload(s.artifactDir, file, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	source := db.DeploySourceUpload
	if currentPrincipal(r).isAPIKey() {
		source = db.DeploySourceApi
	}
	d, err := s.startDeploy(r.Context(), row.Bot.ID, source, art, nil, gitSource{})
	if err != nil {
		writeError(w, r, err)
		return
	}
	s.audit(r, "bot.deploy", "bot", row.Bot.ID.String(), row.Bot.Name, map[string]any{"deploy": d.Number, "source": source, "sha256": art.SHA256})
	writeJSON(w, http.StatusCreated, d)
}

// rollbackDeploy deploys an earlier artifact again as a new deploy.
func (s *Server) rollbackDeploy(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := pathID(r, "deploy")
	if err != nil {
		writeError(w, r, err)
		return
	}
	old, err := s.q.GetDeploy(r.Context(), id)
	if err != nil || old.BotID != row.Bot.ID {
		writeError(w, r, errNotFound)
		return
	}
	if _, err := os.Stat(filepath.Join(s.artifactDir, old.ArtifactSha256+".tar.gz")); err != nil {
		writeError(w, r, errConflict("The code for that deploy is no longer stored on the panel."))
		return
	}
	d, err := s.startDeploy(r.Context(), row.Bot.ID, db.DeploySourceRollback, artifact{SHA256: old.ArtifactSha256, Bytes: old.ArtifactBytes}, &old.ID,
		gitSource{URL: old.GitUrl, Ref: old.GitRef, Commit: old.GitCommit})
	if err != nil {
		writeError(w, r, err)
		return
	}
	s.audit(r, "bot.rollback", "bot", row.Bot.ID.String(), row.Bot.Name, map[string]any{"deploy": d.Number, "to": old.Number})
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) createGitDeploy(w http.ResponseWriter, r *http.Request, botID uuid.UUID, limit int64) {
	var in struct {
		GitURL string `json:"gitUrl"`
		GitRef string `json:"gitRef"`
		Token  string `json:"token"` // for private repositories; used once, never stored
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	repo, err := parseGitURL(in.GitURL)
	if err != nil {
		writeError(w, r, err)
		return
	}
	in.GitRef = strings.TrimSpace(in.GitRef)
	if in.GitRef != "" && !validRef(in.GitRef) {
		writeError(w, r, errBadRequest("That is not a valid branch or tag name."))
		return
	}
	art, src, err := cloneToArtifact(r.Context(), s.artifactDir, repo, in.GitRef, strings.TrimSpace(in.Token), min(limit, maxUploadBytes))
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := s.startDeploy(r.Context(), botID, db.DeploySourceGit, art, nil, src)
	if err != nil {
		writeError(w, r, err)
		return
	}
	s.audit(r, "bot.deploy", "bot", botID.String(), "", map[string]any{"deploy": d.Number, "source": "git", "git": src.URL, "ref": src.Ref, "commit": src.Commit})
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) startDeploy(ctx context.Context, botID uuid.UUID, source db.DeploySource, art artifact, rollbackOf *uuid.UUID, git gitSource) (deployJSON, error) {
	by := currentPrincipalFromCtx(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return deployJSON{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := s.q.WithTx(tx)
	d, err := q.CreateDeploy(ctx, db.CreateDeployParams{BotID: botID, Source: source, RollbackOf: rollbackOf,
		GitUrl: git.URL, GitRef: git.Ref, GitCommit: git.Commit,
		ArtifactSha256: art.SHA256, ArtifactBytes: art.Bytes, CreatedBy: by})
	if err != nil {
		return deployJSON{}, err
	}
	if err := q.SupersedePendingDeploys(ctx, db.SupersedePendingDeploysParams{BotID: botID, ID: d.ID}); err != nil {
		return deployJSON{}, err
	}
	if err := q.SetBotCurrentDeploy(ctx, db.SetBotCurrentDeployParams{ID: botID, CurrentDeployID: &d.ID}); err != nil {
		return deployJSON{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return deployJSON{}, err
	}
	s.hub.pushBot(context.WithoutCancel(ctx), botID)
	return toDeployJSON(d, nil, &d.ID), nil
}

func currentPrincipalFromCtx(ctx context.Context) *uuid.UUID {
	if p, ok := ctx.Value(ctxKey{}).(*principal); ok {
		id := p.user.ID
		return &id
	}
	return nil
}
