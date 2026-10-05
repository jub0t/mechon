package panel

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/proto"
)

const nodeTokenPrefix = "mn_"

type capacityJSON struct {
	MemoryMB      int32 `json:"memoryMb"`
	CPUMillicores int32 `json:"cpuMillicores"`
	DiskMB        int32 `json:"diskMb"`
}

type nodeJSON struct {
	ID           uuid.UUID    `json:"id"`
	Name         string       `json:"name"`
	Region       string       `json:"region"`
	Capacity     capacityJSON `json:"capacity"`
	Allocated    capacityJSON `json:"allocated"`
	Overcommit   float64      `json:"overcommit"`
	Maintenance  bool         `json:"maintenance"`
	BotCount     int32        `json:"botCount"`
	AgentVersion string       `json:"agentVersion"`
	Online       bool         `json:"online"`
	LastSeenAt   *time.Time   `json:"lastSeenAt"`
	CreatedAt    time.Time    `json:"createdAt"`
	// Live, from the agent, while it is connected.
	Host  *proto.Hello     `json:"host,omitempty"`
	Usage *proto.NodeStats `json:"usage,omitempty"`
}

func (s *Server) nodeView(n db.Node, bots int32, alloc capacityJSON) nodeJSON {
	v := nodeJSON{
		ID: n.ID, Name: n.Name, Region: n.Region,
		Capacity:   capacityJSON{n.MemoryMb, n.CpuMillicores, n.DiskMb},
		Allocated:  alloc,
		Overcommit: n.Overcommit, Maintenance: n.Maintenance, BotCount: bots,
		AgentVersion: n.AgentVersion, LastSeenAt: n.LastSeenAt, CreatedAt: n.CreatedAt,
	}
	if live, ok := s.hub.online(n.ID); ok {
		v.Online = true
		hello := live.Hello
		hello.Bots = nil
		v.Host = &hello
		if !live.StatsAt.IsZero() {
			st := live.Stats
			v.Usage = &st
		}
	}
	return v
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListNodes(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]nodeJSON, 0, len(rows))
	for _, n := range rows {
		out = append(out, s.nodeView(db.Node{ID: n.ID, Name: n.Name, Region: n.Region, MemoryMb: n.MemoryMb, CpuMillicores: n.CpuMillicores,
			DiskMb: n.DiskMb, Overcommit: n.Overcommit, Maintenance: n.Maintenance, AgentVersion: n.AgentVersion,
			LastSeenAt: n.LastSeenAt, CreatedAt: n.CreatedAt}, n.BotCount,
			capacityJSON{n.AllocatedMemoryMb, n.AllocatedCpuMillicores, n.AllocatedDiskMb}))
	}
	writeJSON(w, http.StatusOK, out)
}

type nodeInput struct {
	Name          string  `json:"name"`
	Region        string  `json:"region"`
	MemoryMB      int32   `json:"memoryMb"`
	CPUMillicores int32   `json:"cpuMillicores"`
	DiskMB        int32   `json:"diskMb"`
	Overcommit    float64 `json:"overcommit"`
	Maintenance   bool    `json:"maintenance"`
}

func (in *nodeInput) validate() error {
	var err error
	if in.Name, err = normName(in.Name, 40); err != nil {
		return err
	}
	in.Region = strings.TrimSpace(in.Region)
	if in.MemoryMB < 256 || in.CPUMillicores < 250 || in.DiskMB < 1024 {
		return errBadRequest("A node needs at least 256 MB of memory, 0.25 cores and 1 GB of disk to sell.")
	}
	if in.Overcommit == 0 {
		in.Overcommit = 1
	}
	if in.Overcommit < 1 || in.Overcommit > 10 {
		return errBadRequest("Overcommit must be between 1 and 10.")
	}
	return nil
}

// agentSetup is what an operator needs to start the agent on a new server. The token is only
// ever shown here.
type agentSetup struct {
	Token    string `json:"token"`
	PanelURL string `json:"panelUrl"`
	Command  string `json:"command"`
}

func (s *Server) setupFor(token string) agentSetup {
	url := s.cfg.PublicURL.String()
	return agentSetup{
		Token:    token,
		PanelURL: url,
		Command: "curl -fsSL https://raw.githubusercontent.com/jub0t/mechon/main/scripts/install-agent.sh | sudo MECHON_PANEL_URL=" +
			url + " MECHON_NODE_TOKEN=" + token + " sh",
	}
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, r, err)
		return
	}
	token := nodeTokenPrefix + auth.NewToken()
	n, err := s.q.CreateNode(r.Context(), db.CreateNodeParams{Name: in.Name, Region: in.Region, TokenHash: auth.HashToken(token),
		MemoryMb: in.MemoryMB, CpuMillicores: in.CPUMillicores, DiskMb: in.DiskMB, Overcommit: in.Overcommit})
	if uniqueViolation(err) {
		writeError(w, r, errConflict("A node with that name already exists."))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"node": s.nodeView(n, 0, capacityJSON{}), "setup": s.setupFor(token)})
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in nodeInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, r, err)
		return
	}
	n, err := s.q.UpdateNode(r.Context(), db.UpdateNodeParams{ID: id, Name: in.Name, Region: in.Region, MemoryMb: in.MemoryMB,
		CpuMillicores: in.CPUMillicores, DiskMb: in.DiskMB, Overcommit: in.Overcommit, Maintenance: in.Maintenance})
	if uniqueViolation(err) {
		writeError(w, r, errConflict("A node with that name already exists."))
		return
	}
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	writeJSON(w, http.StatusOK, s.nodeView(n, 0, capacityJSON{}))
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	n, err := s.q.CountNodeBots(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if n > 0 {
		writeError(w, r, errConflict("This node still runs bots. Delete or move them first."))
		return
	}
	if err := s.q.DeleteNode(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	if c := s.hub.conn(id); c != nil {
		c.ws.CloseNow()
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateNodeToken issues a new token; the old one stops working at once and the agent is dropped.
func (s *Server) rotateNodeToken(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := s.q.GetNode(r.Context(), id); err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	token := nodeTokenPrefix + auth.NewToken()
	if err := s.q.RotateNodeToken(r.Context(), db.RotateNodeTokenParams{ID: id, TokenHash: auth.HashToken(token)}); err != nil {
		writeError(w, r, err)
		return
	}
	if c := s.hub.conn(id); c != nil {
		c.ws.CloseNow()
	}
	writeJSON(w, http.StatusOK, s.setupFor(token))
}
