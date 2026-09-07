package main

import "time"

// Audit is embedded so the generated schema must flatten anonymous fields.
type Audit struct {
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Todo covers the schema shapes the profiler derives: embedded structs, renamed
// and skipped JSON fields, unexported fields, pointers, slices, maps, and a
// nested struct type.
type Todo struct {
	Audit

	ID        int               `json:"id"`
	Title     string            `json:"title"`
	Completed bool              `json:"completed"`
	Priority  float64           `json:"priority"`
	DueAt     *time.Time        `json:"dueAt"`
	Tags      []string          `json:"tags"`
	Labels    []Label           `json:"labels"`
	Metadata  map[string]string `json:"metadata"`
	Untagged  string
	Internal  string `json:"-"`
	secret    string
}

type CreateTodoRequest struct {
	Title    string   `json:"title"`
	Tags     []string `json:"tags"`
	Priority float64  `json:"priority"`
}

// TodoAlias exercises alias resolution through the semantic type checker.
type TodoAlias = Todo

func (t Todo) hidden() string { return t.secret }
