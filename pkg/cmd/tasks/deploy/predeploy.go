package deploy

import (
	"context"
	"fmt"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/airplanedev/cli/pkg/api"
	"github.com/airplanedev/cli/pkg/configs"
	"github.com/airplanedev/cli/pkg/logger"
	"github.com/airplanedev/cli/pkg/taskdir/definitions"
	"github.com/airplanedev/cli/pkg/utils"
	"github.com/pkg/errors"
)

// ensureConfigVarsExist checks for config references in env and asks users to create any missing ones
func ensureConfigVarsExist(ctx context.Context, client api.APIClient, def definitions.DefinitionInterface) error {
	// Check if env vars exist
	env, err := def.GetEnv()
	if err != nil {
		return err
	}
	for k, v := range env {
		if v.Config != nil {
			if err := ensureConfigVarExists(ctx, client, k, *v.Config); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureConfigVarExists(ctx context.Context, client api.APIClient, envName, configName string) error {
	cn, err := configs.ParseName(configName)
	if err != nil {
		return err
	}
	_, err = client.GetConfig(ctx, api.GetConfigRequest{
		Name: cn.Name,
		Tag:  cn.Tag,
	})
	if err == nil {
		return nil
	}
	switch err := errors.Cause(err).(type) {
	case api.Error:
		if err.Code != 404 {
			return err
		}
		if !utils.CanPrompt() {
			return errors.Errorf("config %s does not exist", configName)
		}
		logger.Log("Your task definition references config %s, which does not exist", logger.Bold(configName))
		confirmed, errc := utils.Confirm("Create it now?")
		if errc != nil {
			return errc
		}
		if !confirmed {
			return errors.Errorf("config %s does not exist", configName)
		}
		return createConfig(ctx, client, cn)
	default:
		return err
	}
}

func createConfig(ctx context.Context, client api.APIClient, cn configs.NameTag) error {
	var secret bool
	if err := survey.AskOne(
		&survey.Confirm{
			Message: "Is this config a secret?",
			Help:    "Secret config values are not shown to users",
			Default: false,
		},
		&secret,
		survey.WithStdio(os.Stdin, os.Stderr, os.Stderr),
	); err != nil {
		return errors.Wrap(err, "prompting value")
	}
	value, err := configs.ReadValueFromPrompt(fmt.Sprintf("Value for %s", configs.JoinName(cn)), secret)
	if err != nil {
		return err
	}
	return configs.SetConfig(ctx, client, cn, value, secret)
}
