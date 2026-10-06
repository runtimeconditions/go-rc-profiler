package main

import (
	"github.com/example/runtimeconditions/semantic-imported-symbols/models"
	"github.com/example/runtimeconditions/semantic-imported-symbols/settings"
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

type RequestAlias = models.CreateTodoRequest

var _ = common.API(settings.APIName,
	common.POST(settings.TodoPath, common.Request[RequestAlias](), common.Response[models.Todo]()),
	env.Env("baseUrl", settings.BaseURLEnv),
)
