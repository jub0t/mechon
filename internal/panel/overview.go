package panel

import (
	"net/http"
)

// overview is the admin dashboard summary.
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	states, err := s.q.CountBotsByState(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	nodes, err := s.q.ListNodes(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	users, err := s.q.ListUsers(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	plans, err := s.q.ListPlans(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	byState := map[string]int32{}
	var bots int32
	for _, st := range states {
		byState[string(st.ObservedState)] = st.N
		bots += st.N
	}
	var online int
	var cap, alloc capacityJSON
	for _, n := range nodes {
		if _, ok := s.hub.online(n.ID); ok {
			online++
		}
		cap.MemoryMB += n.MemoryMb
		cap.CPUMillicores += n.CpuMillicores
		cap.DiskMB += n.DiskMb
		alloc.MemoryMB += n.AllocatedMemoryMb
		alloc.CPUMillicores += n.AllocatedCpuMillicores
		alloc.DiskMB += n.AllocatedDiskMb
	}
	customers := 0
	for _, u := range users {
		if u.Role == "user" {
			customers++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bots":        bots,
		"botsByState": byState,
		"nodes":       len(nodes),
		"nodesOnline": online,
		"users":       customers,
		"plans":       len(plans),
		"capacity":    cap,
		"allocated":   alloc,
	})
}
