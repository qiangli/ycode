package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.groups.List())
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, &httpError{"group id required"})
		return
	}
	g, err := s.groups.Create(body.ID, body.Name)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g := s.groups.Get(id)
	if g == nil {
		writeError(w, http.StatusNotFound, &httpError{"group not found"})
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.groups.Delete(id)
	// Also remove from hub.
	s.hub.mu.Lock()
	delete(s.hub.groups, id)
	s.hub.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddSessionToGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	sessionID := r.PathValue("sid")
	s.groups.AddSession(groupID, sessionID)
	s.hub.AddToGroup(groupID, sessionID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveSessionFromGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	sessionID := r.PathValue("sid")
	s.groups.RemoveSession(groupID, sessionID)
	s.hub.RemoveFromGroup(groupID, sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// httpError implements error for simple HTTP error responses.
type httpError struct {
	msg string
}

func (e *httpError) Error() string { return e.msg }
