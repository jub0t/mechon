package panel

import (
	"net/http"
	"time"
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

// activity feeds the overview charts: fleet usage per minute and deploys per day.
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	span := map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}[r.URL.Query().Get("range")]
	if span == 0 {
		span = 24 * time.Hour
	}
	now := s.now()
	fleet, err := s.q.FleetMetrics(r.Context(), now.Add(-span))
	if err != nil {
		writeError(w, r, err)
		return
	}
	days, err := s.q.DeploysPerDay(r.Context(), now.AddDate(0, 0, -13).Truncate(24*time.Hour))
	if err != nil {
		writeError(w, r, err)
		return
	}
	type point struct {
		T        int64   `json:"t"`
		Memory   int64   `json:"memory"`
		CPUCores float64 `json:"cpuCores"`
		Bots     int32   `json:"bots"`
	}
	type day struct {
		Day       int64 `json:"day"`
		Succeeded int32 `json:"succeeded"`
		Failed    int32 `json:"failed"`
	}
	fp := make([]point, 0, len(fleet))
	for _, f := range fleet {
		fp = append(fp, point{f.Ts.UnixMilli(), f.MemoryBytes, f.CpuCores, f.Bots})
	}
	dp := make([]day, 0, len(days))
	for _, d := range days {
		dp = append(dp, day{d.Day.UnixMilli(), d.Succeeded, d.Failed})
	}
	writeJSON(w, http.StatusOK, map[string]any{"fleet": fp, "deploys": dp})
}
