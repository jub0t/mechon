package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jub0t/mechon/internal/proto"
)

// The file manager: browse and edit a bot's files. The agent does the work on the node, confined
// to the bot's volume; the panel checks access and the path shape first as a second line.

// validFilePath accepts "" (the two roots) or a clean path under app/ or data/.
func validFilePath(p string, allowRoot bool) bool {
	if p == "" {
		return allowRoot
	}
	if len(p) > 1024 || strings.ContainsAny(p, "\x00\\") || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	parts := strings.Split(p, "/")
	if parts[0] != "app" && parts[0] != "data" {
		return false
	}
	if len(parts) == 1 {
		return allowRoot
	}
	for _, s := range parts[1:] {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}

var errBadPath = errBadRequest("That path is not valid. Paths start with app/ or data/.")

// botRequest sends a request to the node running a bot and returns the reply data.
func (h *Hub) botRequest(ctx context.Context, nodeID string, typ string, v any) (json.RawMessage, error) {
	row, err := h.nodeConnByString(nodeID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	data, err := row.request(ctx, typ, v)
	if err != nil {
		return nil, &apiError{http.StatusBadGateway, "agent_error", strings.TrimPrefix(err.Error(), "agent: ")}
	}
	return data, nil
}

func (s *Server) filesRequest(w http.ResponseWriter, r *http.Request, write bool, typ string, body any, path string, allowRoot bool) (json.RawMessage, bool) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return nil, false
	}
	if !validFilePath(path, allowRoot) {
		writeError(w, r, errBadPath)
		return nil, false
	}
	switch v := body.(type) {
	case *proto.FilesPath:
		v.BotID = row.Bot.ID.String()
	case *proto.FileWrite:
		v.BotID = row.Bot.ID.String()
	}
	data, err := s.hub.botRequest(r.Context(), row.Bot.NodeID.String(), typ, body)
	if err != nil {
		writeError(w, r, err)
		return nil, false
	}
	if write {
		s.audit(r, "bot.file."+strings.TrimPrefix(typ, "files."), "bot", row.Bot.ID.String(), row.Bot.Name, map[string]any{"path": path})
	}
	return data, true
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	data, ok := s.filesRequest(w, r, false, proto.TypeFilesList, &proto.FilesPath{Path: p}, p, true)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// readFile returns text content as a string; binary or oversized files come back without content.
func (s *Server) readFile(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	data, ok := s.filesRequest(w, r, false, proto.TypeFilesRead, &proto.FilesPath{Path: p}, p, false)
	if !ok {
		return
	}
	var fc proto.FileContent
	if err := json.Unmarshal(data, &fc); err != nil {
		writeError(w, r, err)
		return
	}
	text := ""
	if !fc.Binary && !fc.Truncated {
		if !utf8.Valid(fc.Content) {
			fc.Binary = true
		} else {
			text = string(fc.Content)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": fc.Path, "size": fc.Size, "modTime": fc.ModTime, "binary": fc.Binary, "truncated": fc.Truncated, "content": text,
	})
}

func (s *Server) writeFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, proto.MaxFileWrite*2)
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if len(in.Content) > proto.MaxFileWrite {
		writeError(w, r, errBadRequest("Files edited here can be up to 1 MB. Deploy larger files instead."))
		return
	}
	if _, ok := s.filesRequest(w, r, true, proto.TypeFilesWrite, &proto.FileWrite{Path: in.Path, Content: []byte(in.Content)}, in.Path, false); ok {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if _, ok := s.filesRequest(w, r, true, proto.TypeFilesDelete, &proto.FilesPath{Path: p}, p, false); ok {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) makeDir(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if _, ok := s.filesRequest(w, r, true, proto.TypeFilesMkdir, &proto.FilesPath{Path: in.Path}, in.Path, false); ok {
		w.WriteHeader(http.StatusNoContent)
	}
}
