package build

import (
	"context"

	"github.com/airplanedev/cli/pkg/api"
	"github.com/airplanedev/cli/pkg/configs"
	"github.com/airplanedev/cli/pkg/logger"
	"github.com/airplanedev/cli/pkg/taskdir"
	"github.com/airplanedev/cli/pkg/taskdir/definitions"
	"github.com/pkg/errors"
)

func Local(ctx context.Context, client *api.Client, dir taskdir.TaskDirectory, def definitions.Definition, taskID string) error {
	registry, err := client.GetRegistryToken(ctx)
	if err != nil {
		return errors.Wrap(err, "getting registry token")
	}

	buildEnv, err := getBuildEnv(ctx, client, def)
	if err != nil {
		return err
	}

	kind, options, err := def.GetKindAndOptions()
	if err != nil {
		return err
	}
	b, err := New(LocalConfig{
		Root:    dir.DefinitionRootPath(),
		Builder: kind,
		Args:    Args(options),
		Auth: &RegistryAuth{
			Token: registry.Token,
			Repo:  registry.Repo,
		},
		BuildEnv: buildEnv,
	})
	if err != nil {
		return errors.Wrap(err, "new build")
	}

	logger.Log("Building...")
	bo, err := b.Build(ctx, taskID, "latest")
	if err != nil {
		return errors.Wrap(err, "build")
	}

	logger.Log("Pushing...")
	if err := b.Push(ctx, bo.Tag); err != nil {
		return errors.Wrap(err, "push")
	}

	return nil
}

// Retreives a build env from def - looks for env vars starting with BUILD_ and either uses the
// string literal or looks up the config value.
func getBuildEnv(ctx context.Context, client *api.Client, def definitions.Definition) (map[string]string, error) {
	buildEnv := make(map[string]string)
	for k, v := range def.Env {
		if v.Value != nil {
			buildEnv[k] = *v.Value
		} else if v.Config != nil {
			nt, err := configs.ParseName(*v.Config)
			if err != nil {
				return nil, err
			}
			res, err := client.GetConfig(ctx, api.GetConfigRequest{
				Name:       nt.Name,
				Tag:        nt.Tag,
				ShowSecret: true,
			})
			if err != nil {
				return nil, err
			}
			buildEnv[k] = res.Config.Value
		}
	}
	return buildEnv, nil
}
