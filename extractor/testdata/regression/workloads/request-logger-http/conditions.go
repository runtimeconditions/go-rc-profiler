package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

func declaration() {
	if 1 != 1 {
		common.API("todos-api",
			common.Spec("openapi", "catalog://api/default/todos-api", "1.0.0"),
			common.GET("/todos/{id}", common.Response[Todo]()),
			env.Env("baseUrl", "TODOS_API_URL"),
		)
		common.Cache("request-cache",
			common.KeyValue(common.Redis),
			env.EnvAlternative(env.Env("url", "REDIS_URL")),
			env.EnvAlternative(
				env.Env("hostname", "REDIS_HOST"),
				env.Env("port", "REDIS_PORT"),
			),
		)
	}
}
