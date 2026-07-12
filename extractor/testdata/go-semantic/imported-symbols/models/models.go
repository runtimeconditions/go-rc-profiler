package models

type CreateTodoRequest struct {
	UserID int    `json:"userId"`
	Title  string `json:"title"`
}

type Todo struct {
	ID       int  `json:"id"`
	Complete bool `json:"completed"`
}
