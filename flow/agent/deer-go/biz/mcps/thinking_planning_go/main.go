package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---------------- Session & Managers ----------------

type ThoughtEvaluation struct {
	Quality     float64  `json:"quality"`
	Novelty     float64  `json:"novelty"`
	Feasibility float64  `json:"feasibility"`
	Confidence  float64  `json:"confidence"`
	Evidence    []string `json:"evidence"`
}

type ThoughtNode struct {
	ID         string             `json:"id"`
	Thought    string             `json:"thought"`
	Depth      int                `json:"depth"`
	ParentID   string             `json:"parentId,omitempty"`
	Children   []string           `json:"children"`
	Evaluation *ThoughtEvaluation `json:"evaluation,omitempty"`
	NodeType   string             `json:"nodeType"`
	BranchID   string             `json:"branchId"`
	CreatedAt  time.Time          `json:"createdAt"`
	Expanded   bool               `json:"expanded"`
}

type ThoughtTreeState struct {
	RootProblem        string                  `json:"rootProblem"`
	Nodes              map[string]*ThoughtNode `json:"nodes"`
	RootNodeID         string                  `json:"rootNodeId"`
	CurrentFocus       string                  `json:"currentFocus"`
	BestSolution       string                  `json:"bestSolution,omitempty"`
	Branches           map[string][]string     `json:"branches"`
	EvaluationCriteria map[string]float64      `json:"evaluationCriteria"`
}

func NewThoughtTreeState(problem string) *ThoughtTreeState {
	root := &ThoughtNode{ID: "root", Thought: problem, Depth: 0, NodeType: "problem", CreatedAt: time.Now()}
	return &ThoughtTreeState{
		RootProblem:  problem,
		Nodes:        map[string]*ThoughtNode{"root": root},
		RootNodeID:   "root",
		CurrentFocus: "root",
		Branches:     map[string][]string{},
		EvaluationCriteria: map[string]float64{
			"quality": 0.4, "feasibility": 0.4, "novelty": 0.2,
		},
	}
}

type SMARTCriteria struct {
	Specific   string `json:"specific"`
	Measurable string `json:"measurable"`
	Achievable string `json:"achievable"`
	Relevant   string `json:"relevant"`
	TimeBound  string `json:"timeBound"`
}

type GoalNode struct {
	ID           string         `json:"id"`
	Description  string         `json:"description"`
	ParentID     string         `json:"parentId,omitempty"`
	Children     []string       `json:"children"`
	SMART        *SMARTCriteria `json:"smartCriteria,omitempty"`
	Status       string         `json:"status"`
	Priority     string         `json:"priority"`
	DeadlineUnix int64          `json:"deadline,omitempty"`
	Resources    []string       `json:"resources"`
	CreatedAt    time.Time      `json:"createdAt"`
	Weight       float64        `json:"weight"`
}

type GoalTreeState struct {
	RootGoal     *GoalNode            `json:"rootGoal"`
	Nodes        map[string]*GoalNode `json:"nodes"`
	CurrentFocus string               `json:"currentFocus"`
	Progress     float64              `json:"progress"`
}

func NewGoalTreeState() *GoalTreeState { return &GoalTreeState{Nodes: map[string]*GoalNode{}} }

type Task struct {
	ID             string    `json:"id"`
	Description    string    `json:"description"`
	Status         string    `json:"status"`
	Priority       string    `json:"priority"`
	GoalID         string    `json:"goalId"`
	EstimatedHours float64   `json:"estimatedHours"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type TaskListState struct {
	Tasks       []*Task `json:"tasks"`
	Progress    float64 `json:"progress"`
	NextTaskNum int     `json:"nextTaskNum"`
}

func NewTaskListState() *TaskListState { return &TaskListState{Tasks: []*Task{}, NextTaskNum: 1} }

type ProjectSession struct {
	ID           string            `json:"id"`
	Vision       string            `json:"vision"`
	ThoughtTree  *ThoughtTreeState `json:"thoughtTree"`
	GoalTree     *GoalTreeState    `json:"goalTree"`
	TaskList     *TaskListState    `json:"taskList"`
	CurrentPhase string            `json:"currentPhase"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*ProjectSession
}

func NewSessionStore() *SessionStore { return &SessionStore{sessions: map[string]*ProjectSession{}} }

func (s *SessionStore) Create(vision string) *ProjectSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.New().String()
	ps := &ProjectSession{
		ID:           id,
		Vision:       vision,
		ThoughtTree:  NewThoughtTreeState(vision),
		GoalTree:     NewGoalTreeState(),
		TaskList:     NewTaskListState(),
		CurrentPhase: "exploring",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	s.sessions[id] = ps
	return ps
}

func (s *SessionStore) Get(id string) (*ProjectSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ps, ok := s.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return ps, nil
}

// ---------------- Utilities ----------------

func setBestSolution(ps *ProjectSession, nodeID string) error {
	if _, ok := ps.ThoughtTree.Nodes[nodeID]; !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	ps.ThoughtTree.BestSolution = nodeID
	ps.CurrentPhase = "planning"
	ps.UpdatedAt = time.Now()
	return nil
}

func addThought(ps *ProjectSession, parentID, thought, nodeType, branchID string) (*ThoughtNode, error) {
	parent, ok := ps.ThoughtTree.Nodes[parentID]
	if !ok {
		return nil, fmt.Errorf("parent node not found: %s", parentID)
	}
	id := fmt.Sprintf("node_%d", len(ps.ThoughtTree.Nodes))
	n := &ThoughtNode{ID: id, Thought: thought, Depth: parent.Depth + 1, ParentID: parentID, NodeType: nodeType, BranchID: branchID, CreatedAt: time.Now()}
	ps.ThoughtTree.Nodes[id] = n
	parent.Children = append(parent.Children, id)
	parent.Expanded = true
	ps.UpdatedAt = time.Now()
	return n, nil
}

func evalThought(ps *ProjectSession, nodeID string, ev *ThoughtEvaluation) error {
	n, ok := ps.ThoughtTree.Nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	n.Evaluation = ev
	ps.UpdatedAt = time.Now()
	return nil
}

func createGoalTree(ps *ProjectSession, solution string) *GoalNode {
	root := &GoalNode{ID: "goal_root", Description: solution, Status: "planned", Priority: "high", CreatedAt: time.Now(), Weight: 1}
	ps.GoalTree.RootGoal = root
	ps.GoalTree.Nodes = map[string]*GoalNode{"goal_root": root}
	ps.GoalTree.CurrentFocus = root.ID
	ps.CurrentPhase = "executing"
	ps.UpdatedAt = time.Now()
	return root
}

func addSubGoal(ps *ProjectSession, parentID, description, priority string, smart *SMARTCriteria) (*GoalNode, error) {
	parent, ok := ps.GoalTree.Nodes[parentID]
	if !ok {
		return nil, fmt.Errorf("parent goal not found: %s", parentID)
	}
	id := fmt.Sprintf("goal_%d", len(ps.GoalTree.Nodes))
	g := &GoalNode{ID: id, Description: description, ParentID: parentID, Status: "planned", Priority: priority, SMART: smart, CreatedAt: time.Now(), Weight: 1}
	ps.GoalTree.Nodes[id] = g
	parent.Children = append(parent.Children, id)
	ps.UpdatedAt = time.Now()
	return g, nil
}

func updateGoalStatus(ps *ProjectSession, goalID, status string) error {
	g, ok := ps.GoalTree.Nodes[goalID]
	if !ok {
		return fmt.Errorf("goal not found: %s", goalID)
	}
	g.Status = status
	ps.UpdatedAt = time.Now()
	// recompute simple progress
	var done, total float64
	for _, v := range ps.GoalTree.Nodes {
		total += v.Weight
		if v.Status == "completed" {
			done += v.Weight
		}
	}
	if total > 0 {
		ps.GoalTree.Progress = done / total
	}
	return nil
}

func createTask(ps *ProjectSession, goalID, desc, priority string, est float64) (*Task, error) {
	if _, ok := ps.GoalTree.Nodes[goalID]; !ok {
		return nil, fmt.Errorf("goal not found: %s", goalID)
	}
	id := fmt.Sprintf("task_%d", ps.TaskList.NextTaskNum)
	t := &Task{ID: id, Description: desc, Status: "pending", Priority: priority, GoalID: goalID, EstimatedHours: est, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	ps.TaskList.Tasks = append(ps.TaskList.Tasks, t)
	ps.TaskList.NextTaskNum++
	ps.UpdatedAt = time.Now()
	recomputeTaskProgress(ps)
	return t, nil
}

func updateTaskStatus(ps *ProjectSession, taskID, status string) error {
	for _, t := range ps.TaskList.Tasks {
		if t.ID == taskID {
			t.Status = status
			t.UpdatedAt = time.Now()
			ps.UpdatedAt = time.Now()
			recomputeTaskProgress(ps)
			return nil
		}
	}
	return fmt.Errorf("task not found: %s", taskID)
}

func recomputeTaskProgress(ps *ProjectSession) {
	if len(ps.TaskList.Tasks) == 0 {
		ps.TaskList.Progress = 0
		return
	}
	var done int
	for _, t := range ps.TaskList.Tasks {
		if t.Status == "completed" {
			done++
		}
	}
	ps.TaskList.Progress = float64(done) / float64(len(ps.TaskList.Tasks))
}

func autoCreateTasksForAllGoals(ps *ProjectSession) error {
	for _, g := range ps.GoalTree.Nodes {
		if g.Status == "planned" {
			if _, err := createTask(ps, g.ID, g.Description, g.Priority, 4.0); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------- MCP Server ----------------

func main() {
	_ = NewSessionStore() // no-op; server is stateless now
	s := server.NewMCPServer("thinking-planning-go", "1.0.0")

	// Thought Tree tool
	thoughtTool := mcp.NewTool("thought_tree",
		mcp.WithDescription("Stateless ToT tool: submit a complete thought tree in one call; echo arguments"),
		mcp.WithString("session_id", mcp.Description("Existing session id; omit when creating")),
		mcp.WithString("operation", mcp.Required(), mcp.Description("create_session | submit_tree")),
		mcp.WithString("vision", mcp.Description("Project vision when creating a session")),
		mcp.WithString("parent_id", mcp.Description("Parent node id for add_thought")),
		mcp.WithString("thought", mcp.Description("Thought content")),
		mcp.WithString("node_type", mcp.Description("problem | hypothesis | reasoning | solution | question")),
		mcp.WithString("branch_id", mcp.Description("Branch label to group nodes")),
		mcp.WithString("node_id", mcp.Description("Target node id for evaluate or set_best_solution")),
		mcp.WithNumber("quality", mcp.Description("0-1 quality score")),
		mcp.WithNumber("novelty", mcp.Description("0-1 novelty score")),
		mcp.WithNumber("feasibility", mcp.Description("0-1 feasibility score")),
		mcp.WithNumber("confidence", mcp.Description("0-1 confidence score")),
		mcp.WithObject("tree", mcp.Description("Full thought tree submitted at once as an array-like object: [{id, parentId, thought, nodeType, score?}, ...]")),
		mcp.WithObject("criteria", mcp.Description("Optional evaluation criteria object")),
	)

	// Goal Tree tool
	goalTool := mcp.NewTool("goal_tree",
		mcp.WithDescription("Stateless Goal Tree tool: submit a complete goal tree in one call; echo arguments"),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Session id")),
		mcp.WithString("operation", mcp.Required(), mcp.Description("create_from_solution | submit_tree")),
		mcp.WithString("solution", mcp.Description("Solution description when creating")),
		mcp.WithString("parent_id", mcp.Description("Parent goal id for add_subgoal")),
		mcp.WithString("description", mcp.Description("Goal description")),
		mcp.WithString("priority", mcp.Description("high | medium | low")),
		mcp.WithString("goal_id", mcp.Description("Target goal id for status update")),
		mcp.WithString("status", mcp.Description("planned | in_progress | completed | blocked")),
		mcp.WithObject("smart_criteria", mcp.Description("SMART fields: specific, measurable, achievable, relevant, timeBound")),
		mcp.WithObject("goals", mcp.Description("Full goal tree submitted at once as an array-like object: [{id, parentId, description, priority, status}, ...]")),
	)

	// Task List tool
	taskTool := mcp.NewTool("task_list",
		mcp.WithDescription("Stateless Task List tool: submit a complete task list in one call; echo arguments"),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Session id")),
		mcp.WithString("operation", mcp.Required(), mcp.Description("submit_list")),
		mcp.WithString("goal_id", mcp.Description("Goal id when creating a task")),
		mcp.WithString("description", mcp.Description("Task description")),
		mcp.WithString("priority", mcp.Description("high | medium | low")),
		mcp.WithString("task_id", mcp.Description("Task id for status update")),
		mcp.WithString("status", mcp.Description("pending | in_progress | completed | blocked")),
		mcp.WithNumber("estimated_hours", mcp.Description("Estimated hours for task")),
		mcp.WithObject("tasks", mcp.Description("Full task list submitted at once as an array-like object: [{id, description, status, priority, goalId, estimatedHours}, ...]")),
	)

	s.AddTool(thoughtTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawArgs := req.Params.Arguments
		args, _ := rawArgs.(map[string]any)
		payload := map[string]any{"tool": "thought_tree", "args": args}
		b, _ := json.Marshal(payload)
		log.Printf("[thought_tree] %s", string(b))
		return mcp.NewToolResultText(string(b)), nil
	})

	s.AddTool(goalTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawArgs := req.Params.Arguments
		args, _ := rawArgs.(map[string]any)
		payload := map[string]any{"tool": "goal_tree", "args": args}
		b, _ := json.Marshal(payload)
		log.Printf("[goal_tree] %s", string(b))
		return mcp.NewToolResultText(string(b)), nil
	})

	s.AddTool(taskTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawArgs := req.Params.Arguments
		args, _ := rawArgs.(map[string]any)
		payload := map[string]any{"tool": "task_list", "args": args}
		b, _ := json.Marshal(payload)
		log.Printf("[task_list] %s", string(b))
		return mcp.NewToolResultText(string(b)), nil
	})

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			// No-op cleanup hook; placeholder for persistence/expiry
		}
	}()

	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}

func getFloat(m map[string]any, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}
func getString(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
