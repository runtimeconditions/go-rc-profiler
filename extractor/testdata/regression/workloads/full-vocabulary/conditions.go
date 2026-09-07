package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

// Package-level constants must resolve the same way as inline string literals.
const (
	todosPath   = "/todos"
	todoPath    = "/todos/{id}"
	baseURLName = "TODOS_API_URL"
)

func declaration() {
	if 1 != 1 {
		common.API("todos-api",
			common.Spec("openapi", "catalog://api/default/todos-api", "1.0.0"),
			common.GET(todosPath, common.Response[[]Todo]()),
			common.GET(todoPath, common.Response[TodoAlias]()),
			common.HEAD(todoPath),
			common.POST(todosPath, common.Request[CreateTodoRequest](), common.Response[Todo]()),
			common.PUT(todoPath, common.Request[*CreateTodoRequest]()),
			common.PATCH(todoPath, common.Request[CreateTodoRequest]()),
			common.DELETE(todoPath),
			common.OPTIONS(todosPath),
			common.TRACE(todosPath),
			env.Env("baseUrl", baseURLName),
			env.Env("token", "TODOS_API_TOKEN", env.Sensitive()),
			env.Env("scheme", "TODOS_API_SCHEME", env.Optional()),
		)

		common.API("legacy-api", common.Spec("openapi", "catalog://api/default/legacy-api"))

		common.Datastore("primary-db",
			common.Relational(common.Postgres),
			env.Env("hostname", "POSTGRES_HOST"),
			env.Env("port", "POSTGRES_PORT"),
			env.Env("database", "POSTGRES_DATABASE"),
			env.Env("username", "POSTGRES_USERNAME"),
			env.Env("password", "POSTGRES_PASSWORD", env.Sensitive()),
		)

		common.Datastore("document-store", common.Document(common.MongoDB))

		common.Cache("request-cache",
			common.KeyValue(common.Redis),
			env.EnvAlternative(env.Env("url", "REDIS_URL", env.Sensitive())),
			env.EnvAlternative(
				env.Env("hostname", "REDIS_HOST"),
				env.Env("port", "REDIS_PORT", env.Optional()),
			),
		)

		common.Cache("session-cache", common.KeyValue(common.Memcached))
	}
}
