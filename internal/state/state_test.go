package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	if s == nil {
		t.Fatal("New() returned nil")
	}

	if s.Repos == nil {
		t.Error("Repos map not initialized")
	}

	if len(s.Repos) != 0 {
		t.Errorf("Repos length = %d, want 0", len(s.Repos))
	}
}

func TestStateSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create state and add a repo
	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	// Add an agent
	agent := Agent{
		Type:         AgentTypeSupervisor,
		WorktreePath: "/path/to/worktree",
		TmuxWindow:   "supervisor",
		SessionID:    "test-session",
		PID:          12345,
		CreatedAt:    time.Now(),
	}

	if err := s.AddAgent("test-repo", "supervisor", agent); err != nil {
		t.Fatalf("AddAgent() failed: %v", err)
	}

	// Load state from disk
	loaded, err := Load(statePath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify repo was loaded
	loadedRepo, exists := loaded.GetRepo("test-repo")
	if !exists {
		t.Fatal("Repository not found after load")
	}

	if loadedRepo.GithubURL != repo.GithubURL {
		t.Errorf("GithubURL = %q, want %q", loadedRepo.GithubURL, repo.GithubURL)
	}

	// Verify agent was loaded
	loadedAgent, exists := loaded.GetAgent("test-repo", "supervisor")
	if !exists {
		t.Fatal("Agent not found after load")
	}

	if loadedAgent.Type != agent.Type {
		t.Errorf("Agent Type = %q, want %q", loadedAgent.Type, agent.Type)
	}

	if loadedAgent.PID != agent.PID {
		t.Errorf("Agent PID = %d, want %d", loadedAgent.PID, agent.PID)
	}
}

func TestLoadNonExistentState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "nonexistent.json")

	s, err := Load(statePath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if len(s.Repos) != 0 {
		t.Errorf("Repos length = %d, want 0 for new state", len(s.Repos))
	}
}

func TestAddRepoDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	// Adding again should fail
	if err := s.AddRepo("test-repo", repo); err == nil {
		t.Error("AddRepo() succeeded for duplicate repo")
	}
}

func TestGetRepoNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)

	_, exists := s.GetRepo("nonexistent")
	if exists {
		t.Error("GetRepo() found nonexistent repo")
	}
}

func TestListRepos(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)

	// Empty list
	repos := s.ListRepos()
	if len(repos) != 0 {
		t.Errorf("ListRepos() length = %d, want 0", len(repos))
	}

	// Add repos
	for i, name := range []string{"repo1", "repo2", "repo3"} {
		repo := &Repository{
			GithubURL:   "https://github.com/test/" + name,
			TmuxSession: "multiclaude-" + name,
			Agents:      make(map[string]Agent),
		}
		if err := s.AddRepo(name, repo); err != nil {
			t.Fatalf("AddRepo(%d) failed: %v", i, err)
		}
	}

	repos = s.ListRepos()
	if len(repos) != 3 {
		t.Errorf("ListRepos() length = %d, want 3", len(repos))
	}
}

func TestAddAgentNonExistentRepo(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)

	agent := Agent{
		Type:         AgentTypeSupervisor,
		WorktreePath: "/path/to/worktree",
		TmuxWindow:   "supervisor",
		SessionID:    "test-session",
		PID:          12345,
		CreatedAt:    time.Now(),
	}

	if err := s.AddAgent("nonexistent", "supervisor", agent); err == nil {
		t.Error("AddAgent() succeeded for nonexistent repo")
	}
}

func TestAddAgentDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	agent := Agent{
		Type:         AgentTypeSupervisor,
		WorktreePath: "/path/to/worktree",
		TmuxWindow:   "supervisor",
		SessionID:    "test-session",
		PID:          12345,
		CreatedAt:    time.Now(),
	}

	if err := s.AddAgent("test-repo", "supervisor", agent); err != nil {
		t.Fatalf("AddAgent() failed: %v", err)
	}

	// Adding again should fail
	if err := s.AddAgent("test-repo", "supervisor", agent); err == nil {
		t.Error("AddAgent() succeeded for duplicate agent")
	}
}

func TestUpdateAgent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	agent := Agent{
		Type:         AgentTypeWorker,
		WorktreePath: "/path/to/worktree",
		TmuxWindow:   "worker",
		SessionID:    "test-session",
		PID:          12345,
		Task:         "Original task",
		CreatedAt:    time.Now(),
	}

	if err := s.AddAgent("test-repo", "worker", agent); err != nil {
		t.Fatalf("AddAgent() failed: %v", err)
	}

	// Update the agent
	agent.ReadyForCleanup = true
	if err := s.UpdateAgent("test-repo", "worker", agent); err != nil {
		t.Fatalf("UpdateAgent() failed: %v", err)
	}

	// Verify update
	updated, exists := s.GetAgent("test-repo", "worker")
	if !exists {
		t.Fatal("Agent not found after update")
	}

	if !updated.ReadyForCleanup {
		t.Error("ReadyForCleanup not updated")
	}
}

func TestRemoveAgent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	agent := Agent{
		Type:         AgentTypeSupervisor,
		WorktreePath: "/path/to/worktree",
		TmuxWindow:   "supervisor",
		SessionID:    "test-session",
		PID:          12345,
		CreatedAt:    time.Now(),
	}

	if err := s.AddAgent("test-repo", "supervisor", agent); err != nil {
		t.Fatalf("AddAgent() failed: %v", err)
	}

	// Remove agent
	if err := s.RemoveAgent("test-repo", "supervisor"); err != nil {
		t.Fatalf("RemoveAgent() failed: %v", err)
	}

	// Verify removal
	_, exists := s.GetAgent("test-repo", "supervisor")
	if exists {
		t.Error("Agent still exists after removal")
	}
}

func TestListAgents(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	// Empty list
	agents, err := s.ListAgents("test-repo")
	if err != nil {
		t.Fatalf("ListAgents() failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("ListAgents() length = %d, want 0", len(agents))
	}

	// Add agents
	agentNames := []string{"supervisor", "merge-queue", "worker1"}
	for _, name := range agentNames {
		agent := Agent{
			Type:         AgentTypeSupervisor,
			WorktreePath: "/path/" + name,
			TmuxWindow:   name,
			SessionID:    "session-" + name,
			PID:          12345,
			CreatedAt:    time.Now(),
		}
		if err := s.AddAgent("test-repo", name, agent); err != nil {
			t.Fatalf("AddAgent(%s) failed: %v", name, err)
		}
	}

	agents, err = s.ListAgents("test-repo")
	if err != nil {
		t.Fatalf("ListAgents() failed: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("ListAgents() length = %d, want 3", len(agents))
	}
}

func TestStateAtomicSave(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath)
	repo := &Repository{
		GithubURL:   "https://github.com/test/repo",
		TmuxSession: "multiclaude-test-repo",
		Agents:      make(map[string]Agent),
	}

	if err := s.AddRepo("test-repo", repo); err != nil {
		t.Fatalf("AddRepo() failed: %v", err)
	}

	// Verify temp file was cleaned up
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temp file not cleaned up after save")
	}

	// Verify state file exists
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Error("State file not created")
	}
}
